// Every command goes the same way (spec §3): POST /api/v2/cmd/<op> with its opId and the device header. The op is
// pending in opsStore at once; past slowMs it is slow («дольше обычного»); the runtime's result or no answer within
// cmdTimeoutMs ends it. An accepted command is done only once its channel has shown the result's seq — the page
// waits for the socket, not for a timer — and a retry sends the same body with the same opId.

import { pageClock } from '@/next/core/clock';
import { uuidv7 } from '@/next/core/ids';
import { postCommand } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { hub } from '@/next/core/realtime/hub';
import { limitsOf } from '@/next/stores/machines';
import { opsStore, type OpFailure, type OpState } from '@/next/stores/ops';
import {
  RETRYABLE_REFUSE_CODES,
  type CmdOutcome,
  type CommandBodies,
  type MachineId,
  type Op,
} from '@/next/protocol/v2';

/** A command's body without its opId; a union body (answer, conversation.create) stays a union. */
export type Body<K extends Op> = CommandBodies[K] extends infer B
  ? B extends unknown
    ? Omit<B, 'opId'>
    : never
  : never;

export interface OpHandle {
  opId: string;
  /** Resolves with the final state: done or failed; never rejects. */
  done: Promise<Extract<OpState, { k: 'done' | 'failed' }>>;
}

interface Sent {
  machine: MachineId;
  op: Op;
  body: Record<string, unknown>;
}

const sent = new Map<string, Sent>();
const running = new Map<string, OpHandle>();

const retryableFailure = (f: OpFailure): boolean => {
  if (f.r === 'refused') return (RETRYABLE_REFUSE_CODES as readonly string[]).includes(f.code);
  if (f.code === 'http') return f.status >= 500 || f.status === 429;
  return true;
};

/** Sends op to machine; opId defaults to a new UUIDv7 (send passes its clientId). */
export function runCommand<K extends Op>(
  machine: MachineId,
  op: K,
  body: Body<K>,
  opId: string = uuidv7(),
): OpHandle {
  const full = { ...body, opId } as Record<string, unknown>;
  sent.set(opId, { machine, op, body: full });
  return execute(opId);
}

/** The same command again, same opId and body: the runtime answers what it answered, or does it now. */
export function retryCommand(opId: string): OpHandle | null {
  const s = sent.get(opId);
  if (!s) return null;
  const cur = running.get(opId);
  if (cur && opsStore.get(opId)?.k === 'pending') return cur;
  return execute(opId);
}

/** What a command was sent with (for the message outbox and tests). */
export const sentBody = (opId: string): Sent | undefined => sent.get(opId);

function execute(opId: string): OpHandle {
  const s = sent.get(opId) as Sent;
  const clock = pageClock();
  const limits = limitsOf(s.machine);
  const since = clock.now();
  opsStore.set(opId, { k: 'pending', op: s.op, machine: s.machine, since, slow: false, stage: 'request' });
  const slowTimer = clock.setTimeout(() => {
    opsStore.patch(opId, (st) => (st?.k === 'pending' && st.since === since ? { ...st, slow: true } : st));
  }, limits.slowMs);

  const done = (async () => {
    const outcome: CmdOutcome = await postCommand(
      machineApi(s.machine).http(`/cmd/${s.op}`),
      s.body,
      limits.cmdTimeoutMs,
    );
    if (outcome.r === 'accepted' || outcome.r === 'queued') {
      const client = hub.client(s.machine);
      if (client?.isSubscribed(outcome.ch)) {
        opsStore.patch(opId, (st) => (st?.k === 'pending' ? { ...st, stage: 'effect' } : st));
        // The effect shows on the socket; a socket that is down shows it after the resume. The wait has the
        // command's term too, so a button never spins longer than cmdTimeoutMs past the answer.
        let timer: unknown;
        await Promise.race([
          client.waitSeq(outcome.ch, outcome.seq),
          new Promise((resolve) => (timer = clock.setTimeout(() => resolve('late'), limits.cmdTimeoutMs))),
        ]);
        clock.clearTimeout(timer);
      }
      const final = { k: 'done', op: s.op, machine: s.machine, result: outcome, at: clock.now() } as const;
      opsStore.set(opId, final);
      return final;
    }
    const failure = outcome as OpFailure;
    const final = {
      k: 'failed',
      op: s.op,
      machine: s.machine,
      failure,
      retryable: retryableFailure(failure),
      at: clock.now(),
    } as const;
    opsStore.set(opId, final);
    return final;
  })().finally(() => clock.clearTimeout(slowTimer));

  const handle = { opId, done };
  running.set(opId, handle);
  return handle;
}

/** Forgets ops that ended long ago (the store keeps the recent ones for the buttons). */
export function forgetOp(opId: string): void {
  opsStore.delete(opId);
  sent.delete(opId);
  running.delete(opId);
}
