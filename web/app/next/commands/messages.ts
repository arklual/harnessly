// Outgoing messages: send, cancel, retry, promote, and the outbox that sends again what had no answer. A message is
// submitting on the page until the runtime shows it (prompt_state or message_upsert). The page never decides that a
// message failed because of the road: no connection, a restarting machine, a gateway 502/503, an answer that did not
// come, a socket that dropped, and the refusals that say «the machine is not there now» (machine-offline, timeout,
// internal, rate-limited) are all «waiting for the machine». The message stays submitting, quietly, and goes again
// with the same clientId — the runtime's opId store makes that safe, it answers what it answered and never types
// twice — on a timer with growing steps and at once when the machine's socket is live again, after F5 too. Only
// a refusal on the merits (too long, queue full, the chat is gone, no rights) ends it in failed, with its reason.
// Whoever leads the delivery — this runtime or a cluster's outbox — says where the message is by an event
// (prompt_state, message_upsert); the page only shows it.
//
// The messages of one chat go one at a time, in the order they were written (commands/send-line): the next one leaves
// when the one before it was answered. A message taken back is «cancelling» until the runtime — and the cluster, for
// its copy — answered: only then is it cancelled.

import { machineNow, pageClock } from '@/next/core/clock';
import { uuidv7 } from '@/next/core/ids';
import { batch } from '@/next/core/store';
import { adoptParked, parkInCluster, unparkInCluster } from '@/next/commands/cluster-queue';
import { startMessageStatus } from '@/next/commands/message-status';
import {
  byMaking,
  forgetAnswered,
  isUnsent,
  nextUnsent,
  noteAnswered,
  waitsItsTurn,
} from '@/next/commands/send-line';
import { runCommand, type OpHandle } from '@/next/commands/run';
import { anyKept, takeKept } from '@/next/commands/uploads';
import { connectionStore } from '@/next/stores/connection';
import { limitsOf } from '@/next/stores/machines';
import { conversationsStore } from '@/next/stores/conversations';
import {
  HOLDS,
  isPassingRefusal,
  isRetryableRefusal,
  isStalled,
  messagesStore,
  forgetMessage,
  moveLocally,
  putLocal,
  type MessageView,
} from '@/next/stores/messages';
import type {
  AttachmentRef,
  ClientId,
  ConvId,
  MachineId,
  MessageKind,
  MessageMode,
  MessageRef,
  PromoteTo,
} from '@/next/protocol/v2';

export interface Outgoing {
  conversationId: ConvId;
  text: string;
  attachments?: AttachmentRef[];
  /** The places it points at (a design, a document…): the runtime writes their block under the text. */
  refs?: MessageRef[];
  mode?: MessageMode;
  kind?: MessageKind;
}

/** The sends on their way, each with the promise of its end. */
const inFlight = new Map<ClientId, Promise<void>>();
/**
 * The messages written on this page that cannot have reached their machine: no try of theirs has left yet, or the only
 * answers were «the machine is off». Taking one of them back needs no word from the machine.
 */
const unreached = new Set<ClientId>();
/** The cancels whose confirmation is being asked for now. */
const cancelRuns = new Set<ClientId>();
/** The unconfirmed messages Повторить already sent again under their own clientId. */
const retriedSame = new Set<ClientId>();
/** The messages written on this page: what it parked of them in the cluster, it knows. */
const bornHere = new Set<ClientId>();
/** Who waits for a cancel to be settled: told true when the message is cancelled, false when the agent has it. */
const cancelWaiters = new Map<ClientId, Array<(cancelled: boolean) => void>>();

/** Tells whoever waits for the cancel of a message that is no longer «cancelling» what became of it. */
function releaseCancel(clientId: ClientId): void {
  const waiters = cancelWaiters.get(clientId);
  const m = messagesStore.get(clientId);
  if (!waiters || m?.cancelling) return;
  cancelWaiters.delete(clientId);
  // Gone from the page: it ended long ago and only a stale copy was left.
  const cancelled = !m || m.state.k === 'cancelled';
  for (const tell of waiters) tell(cancelled);
}

/** How long the page waits before it sends an unanswered message again: 1 s, 2 s … then every 30 s. */
export const RESEND_STEPS_MS = [1000, 2000, 4000, 8000, 15000, 30000] as const;
const resendTimers = new Map<ClientId, unknown>();
const resendCount = new Map<ClientId, number>();

function forgetResend(clientId: ClientId): void {
  pageClock().clearTimeout(resendTimers.get(clientId));
  resendTimers.delete(clientId);
  resendCount.delete(clientId);
}

function scheduleResend(clientId: ClientId): void {
  const m = messagesStore.get(clientId);
  // A message of a chat that is only being created waits for that command, which the person repeats.
  if (!m?.conversationId) return;
  const { machineId, conversationId } = m;
  pageClock().clearTimeout(resendTimers.get(clientId));
  const n = resendCount.get(clientId) ?? 0;
  resendCount.set(clientId, n + 1);
  resendTimers.set(
    clientId,
    pageClock().setTimeout(
      () => {
        resendTimers.delete(clientId);
        resend(clientId);
        // Its try is over or no longer needed: the next of the chat may go.
        advance(machineId, conversationId);
      },
      RESEND_STEPS_MS[Math.min(n, RESEND_STEPS_MS.length - 1)],
    ),
  );
}

const submitWait = (m: MachineId) => {
  const now = machineNow(m);
  const limits = limitsOf(m);
  return { slowAt: now + limits.slowMs, deadlineAt: now + limits.cmdTimeoutMs };
};

/** A new message, submitting at once; the runtime's states follow on the socket. */
export function sendMessage(machine: MachineId, out: Outgoing): ClientId {
  const clientId = uuidv7();
  unreached.add(clientId);
  bornHere.add(clientId);
  putLocal(newView(machine, clientId, out.conversationId, out));
  submit(clientId);
  return clientId;
}

export function newView(
  machine: MachineId,
  clientId: ClientId,
  conversationId: ConvId | null,
  out: Omit<Outgoing, 'conversationId'>,
): MessageView {
  const agent = conversationId ? conversationsStore.get(conversationId)?.state.k : undefined;
  return {
    clientId,
    machineId: machine,
    conversationId,
    text: out.text,
    attachments: out.attachments ?? [],
    refs: out.refs ?? [],
    mode: out.mode ?? 'queue',
    kind: out.kind ?? 'prompt',
    // Written to a busy agent: it stands in the stack over the field from the first moment, not in the feed.
    held: agent !== undefined && HOLDS.has(agent),
    createdAt: machineNow(machine),
    stateAt: machineNow(machine),
    attempts: 0,
    state: { k: 'submitting' },
    rev: 0,
    refused: null,
    wait: submitWait(machine),
    issue: null,
  };
}

/**
 * the messages of this chat that wait in the cluster and this page has not got (written on another device, or
 * before the tab was closed) join the page's own, on their way; the outbox sends them too when the machine is back.
 */
export async function adoptParkedMessages(machine: MachineId, conv: ConvId): Promise<void> {
  const taken = await adoptParked(machine, conv, (send) =>
    newView(machine, send.clientId, conv, {
      text: send.text,
      mode: send.mode,
      kind: send.kind,
      ...(send.refs ? { refs: send.refs } : {}),
    }),
  );
  for (const clientId of taken) submit(clientId);
}

/**
 * Sends (or sends again) a message the runtime has not shown yet — when it is its turn: a message written after one
 * that is still unanswered waits for it (advance sends it then).
 */
function submit(clientId: ClientId): void {
  const m = messagesStore.get(clientId);
  if (!m || !m.conversationId || !isSendable(m) || inFlight.has(clientId)) return;
  // to a machine that is off, the cluster keeps the message too (it outlives the tab, shows on another
  // device, goes by itself when the machine is back); this page goes on sending it as well, under the same opId.
  // A message that waits its turn here is kept there all the same.
  void parkInCluster(clientId);
  forgetAnswered(clientId);
  if (waitsItsTurn(m)) return;
  const { machineId, conversationId } = m;
  const run = dispatch(clientId).finally(() => {
    inFlight.delete(clientId);
    advance(machineId, conversationId);
  });
  inFlight.set(clientId, run);
}

/** The page's own message in submitting that was not taken back: what submit may send. */
const isSendable = (m: MessageView): boolean => m.rev === 0 && m.state.k === 'submitting' && !m.cancelling;

async function dispatch(clientId: ClientId): Promise<void> {
  const m = messagesStore.get(clientId);
  if (!m?.conversationId) return;
  // what was attached while the machine was off is in the hub; the machine takes it before the message goes.
  // A message without them goes in the same frame, as before.
  const attachments = anyKept(m.attachments)
    ? await takeKept(m.machineId, m.attachments)
    : [...m.attachments];
  if (!attachments) {
    scheduleResend(clientId);
    return;
  }
  // Taken back while its attachments were on their way.
  if (messagesStore.get(clientId)?.cancelling) return;
  unreached.delete(clientId);
  const handle = runCommand(
    m.machineId,
    'send',
    {
      clientId,
      conversationId: m.conversationId,
      text: m.text,
      attachmentIds: attachments.map((a) => a.id),
      mode: m.mode,
      kind: m.kind,
      ...(m.refs && m.refs.length > 0 ? { refs: m.refs } : {}),
    },
    clientId,
  );
  const st = await handle.done;
  // The machine is off, by the word of whoever answers for it: this try did not reach it.
  if (st.k === 'failed' && st.failure.r === 'refused' && st.failure.code === 'machine-offline')
    unreached.add(clientId);
  settleSubmit(clientId, st);
}

/** Sends the chat's next unsent message when nothing of it is on its way and no later try is set. */
function advance(machine: MachineId, conv: ConvId): void {
  const next = nextUnsent(machine, conv);
  if (!next || inFlight.has(next.clientId) || resendTimers.has(next.clientId)) return;
  // Its terms count from its turn, not from when it was written: the wait the person sees, and the moment the page
  // would ask the runtime where an unanswered message is.
  messagesStore.patch(
    next.clientId,
    (v) => v && { ...v, wait: submitWait(v.machineId), stateAt: machineNow(v.machineId) },
  );
  submit(next.clientId);
}

/** The answer to send (or to conversation.create with this first message) as the message's page state. */
export function settleSubmit(clientId: ClientId, st: Awaited<OpHandle['done']>): void {
  if (st.k === 'done') {
    // The runtime has it; what it does next comes as its event. The page stops saying it waits for the machine.
    noteAnswered(clientId);
    forgetResend(clientId);
    if (messagesStore.get(clientId)?.issue) moveLocally(clientId, (v) => ({ state: v.state, issue: null }));
    return;
  }
  const f = st.failure;
  const m = messagesStore.get(clientId);
  // The road failed, or the machine said it is not there now: the runtime may have the message or get it later,
  // so it is not refused. It stays submitting («waiting for the machine») and goes again by itself.
  const ofTheRoad = f.r !== 'refused' || (isRetryableRefusal(f.code) && !!m?.conversationId);
  if (ofTheRoad) {
    moveLocally(clientId, (v) => ({ state: { k: 'submitting' }, wait: v.wait, issue: f.code }));
    scheduleResend(clientId);
    return;
  }
  // f.r === 'refused' here: the runtime said no on the merits, and the same clientId would get the same no.
  forgetResend(clientId);
  if (f.code === 'too-late') {
    // The runtime delivered this clientId long ago and let its record go: the page's copy is all that was left of it.
    forgetMessage(clientId);
    return;
  }
  moveLocally(clientId, () => ({
    state: { k: 'failed', code: 'refused', retryable: false, at: st.at },
    refused: f.code,
    wait: null,
    issue: null,
  }));
}

/** Повторить: the page's own retry before the runtime has the message, the runtime's retry after it failed there. */
export function retryMessage(clientId: ClientId): OpHandle | null {
  const m = messagesStore.get(clientId);
  if (!m) return null;
  if (m.cancelling) return null;
  if (m.rev === 0) {
    if (m.state.k === 'failed' && !m.state.retryable) return null;
    const unconfirmed = m.state.k === 'failed' && m.state.code === 'unconfirmed' && !!m.conversationId;
    // The runtime said it does not know this clientId — but the answer may have been about a moment before the send
    // landed, so what became of the message is not known: it goes again under the same clientId, which the runtime
    // takes once however many times it comes. Only when that too came back unconfirmed is it certain the runtime
    // keeps answering from its memory of the opId without having the message: then the words go as a new message.
    if (unconfirmed && retriedSame.has(clientId) && m.conversationId) {
      retriedSame.delete(clientId);
      const next = sendMessage(m.machineId, {
        conversationId: m.conversationId,
        text: m.text,
        attachments: m.attachments,
        refs: m.refs,
        mode: m.mode,
        kind: m.kind,
      });
      messagesStore.patch(next, (v) => v && { ...v, held: true });
      forgetResend(clientId);
      moveLocally(clientId, () => ({ state: { k: 'cancelled', by: 'user' }, wait: null, issue: null }));
      return null;
    }
    if (unconfirmed) retriedSame.add(clientId);
    forgetResend(clientId);
    forgetAnswered(clientId);
    moveLocally(clientId, (v) => ({
      state: { k: 'submitting' },
      wait: submitWait(v.machineId),
      issue: null,
      refused: null,
      attempts: v.attempts + 1,
    }));
    submit(clientId);
    return null;
  }
  // A stalled message (the agent did not take it in time) waits at the head of the queue for this word, like a failed one.
  if ((m.state.k !== 'failed' && !isStalled(m)) || !m.conversationId) return null;
  return runCommand(m.machineId, 'retry', { conversationId: m.conversationId, clientId });
}

/**
 * Отправить ещё раз: a refusal whose cause may have passed (the agent was gone, the machine away) is answered again
 * with the same words and attachments as a new message with a new clientId; the refused one goes. A refusal on the
 * merits would get the same no, so it is not sent again. Returns the new clientId, or null when nothing was sent.
 */
export function resendRefused(clientId: ClientId): ClientId | null {
  const m = messagesStore.get(clientId);
  if (!m || !m.conversationId || m.rev !== 0 || m.state.k !== 'failed' || !isPassingRefusal(m.refused))
    return null;
  const next = sendMessage(m.machineId, {
    conversationId: m.conversationId,
    text: m.text,
    attachments: m.attachments,
    refs: m.refs,
    mode: m.mode,
    kind: m.kind,
  });
  forgetResend(clientId);
  moveLocally(clientId, () => ({ state: { k: 'cancelled', by: 'user' }, wait: null, issue: null }));
  return next;
}

/**
 * Отменить. A message the runtime refused is only the page's, and goes at once. One the runtime may have, or may still
 * get — a try on its way, a try whose answer was lost, a copy in the cluster — is «cancelling» until that is settled
 * (confirmCancel); one the runtime shows is cancelled there.
 */
export function cancelMessage(clientId: ClientId): OpHandle | null {
  const m = messagesStore.get(clientId);
  if (!m || !m.conversationId || m.cancelling) return null;
  const conv = m.conversationId;
  if (m.rev === 0 && m.state.k === 'failed') {
    // The runtime refused it, or said it does not have it: the page's copy is all there is. The cluster's copy of a
    // message the machine has not shown goes with it.
    void unparkInCluster(m);
    forgetResend(clientId);
    moveLocally(clientId, () => ({ state: { k: 'cancelled', by: 'user' }, wait: null, issue: null }));
    return null;
  }
  if (m.rev === 0 && m.state.k === 'submitting') {
    forgetResend(clientId);
    messagesStore.patch(clientId, (v) => v && { ...v, cancelling: true });
    const machine = m.machineId;
    // Whoever asked (Delete, Изменить) waits for the cancel to be settled, as for any command.
    const done = new Promise<Awaited<OpHandle['done']>>((resolve) => {
      const tell = (cancelled: boolean) => {
        const at = pageClock().now();
        resolve(
          cancelled
            ? { k: 'done', op: 'cancel', machine, at, result: { r: 'accepted', ch: `conv:${conv}`, seq: 0 } }
            : {
                k: 'failed',
                op: 'cancel',
                machine,
                at,
                retryable: false,
                failure: { r: 'refused', code: 'too-late' },
              },
        );
      };
      cancelWaiters.set(clientId, [...(cancelWaiters.get(clientId) ?? []), tell]);
    });
    void confirmCancel(clientId);
    // It no longer holds up the messages written after it.
    advance(machine, conv);
    return { opId: clientId, done };
  }
  return forgetIfUnknown(
    clientId,
    conv,
    runCommand(m.machineId, 'cancel', { conversationId: conv, clientId }),
  );
}

/** The cancel is settled: the message is cancelled on the page (the runtime never showed it), or is what the runtime says. */
function endCancel(clientId: ClientId, cancelled: boolean): void {
  forgetResend(clientId);
  const m = messagesStore.get(clientId);
  if (!m?.cancelling) return;
  const { cancelling: _, ...rest } = m;
  batch(() => {
    messagesStore.set(clientId, rest);
    if (cancelled && rest.rev === 0)
      moveLocally(clientId, () => ({ state: { k: 'cancelled', by: 'user' }, wait: null, issue: null }));
  });
  releaseCancel(clientId);
}

/**
 * Settles a cancel. A try on its way is answered first, so the cancel never overtakes its own send. The cluster's copy
 * is dropped (it waits for a park on its way and asks until the cluster answers). Then the runtime is told: it cancels
 * the message if it has it, and remembers the cancel if it does not, so a send that comes later — from the road, or
 * from the cluster — is not queued. Without the machine's answer the message stays «cancelling» and the cancel goes
 * again like an unanswered send; only a message that cannot have reached the machine needs no word from it.
 */
async function confirmCancel(clientId: ClientId): Promise<void> {
  if (cancelRuns.has(clientId)) return;
  cancelRuns.add(clientId);
  try {
    await inFlight.get(clientId);
    const m = messagesStore.get(clientId);
    if (!m?.cancelling || !m.conversationId) return;
    // Of a message this page did not write (kept through a reload, taken from the cluster) it does not know what the
    // cluster keeps: it asks.
    const copy = await unparkInCluster(m, !bornHere.has(clientId));
    if (copy === 'delivered') unreached.delete(clientId);
    else if (copy !== 'unknown' && unreached.has(clientId) && messagesStore.get(clientId)?.rev === 0) {
      // Nothing of it left this page for the machine, and the cluster will not deliver it: it is nowhere else.
      endCancel(clientId, true);
      return;
    }
    const st = await runCommand(m.machineId, 'cancel', { conversationId: m.conversationId, clientId }).done;
    if (st.k === 'done') {
      // The runtime had it and took it out (or shows what became of it): its event says the rest.
      endCancel(clientId, true);
      return;
    }
    const f = st.failure;
    if (f.r !== 'refused' || isRetryableRefusal(f.code)) {
      // The road, or a machine that is not there now: nobody answered for the message yet.
      if (messagesStore.get(clientId)?.cancelling) scheduleResend(clientId);
      return;
    }
    if (f.code === 'not-found' && (messagesStore.get(clientId)?.rev ?? 0) > 0) {
      // The runtime let its record go long ago: the page's stale copy goes too.
      forgetResend(clientId);
      forgetMessage(clientId);
      return;
    }
    // not-found: the runtime does not have it and remembers the cancel. too-late: the agent has it already — the
    // message is what the runtime says it is. Any other no is on the merits and would be said again.
    endCancel(clientId, f.code !== 'too-late');
  } finally {
    cancelRuns.delete(clientId);
    releaseCancel(clientId);
  }
}

/**
 * The runtime does not know the message: it never had it (the page's copy is all there is, and it is cancelled), or it
 * had it and let its finished record go (the page kept a stale copy) — then the copy goes. Either way the
 * person's Delete, «Сразу» or Изменить of it is done, not «не получилось».
 */
export function forgetIfUnknown(clientId: ClientId, conv: ConvId, handle: OpHandle): OpHandle {
  const done = handle.done.then((st): Awaited<OpHandle['done']> => {
    if (st.k !== 'failed' || st.failure.r !== 'refused' || st.failure.code !== 'not-found') return st;
    const m = messagesStore.get(clientId);
    if (m && m.rev === 0)
      moveLocally(clientId, () => ({ state: { k: 'cancelled', by: 'user' }, wait: null, issue: null }));
    else forgetMessage(clientId);
    const { op, machine, at } = st;
    return { k: 'done', op, machine, at, result: { r: 'accepted', ch: `conv:${conv}`, seq: 0 } };
  });
  return { ...handle, done };
}

/** «Сразу»: the queued message goes next (now) or into the running turn (steer). */
export function promoteMessage(clientId: ClientId, to: PromoteTo): OpHandle | null {
  const m = messagesStore.get(clientId);
  if (!m || !m.conversationId || m.rev === 0) return null;
  const conv = m.conversationId;
  return forgetIfUnknown(
    clientId,
    conv,
    runCommand(m.machineId, 'promote', { conversationId: conv, clientId, to }),
  );
}

/**
 * Sends an unanswered message again, keeping what the page said of the last try until the new one is answered; a
 * cancel that was not answered is asked again instead.
 */
function resend(clientId: ClientId): void {
  const m = messagesStore.get(clientId);
  if (m?.cancelling) {
    void confirmCancel(clientId);
    return;
  }
  if (!m || m.rev !== 0 || m.state.k !== 'submitting' || inFlight.has(clientId)) return;
  batch(() => moveLocally(clientId, (v) => ({ state: { k: 'submitting' }, wait: submitWait(v.machineId) })));
  submit(clientId);
}

/**
 * Sends again every message of machine that is submitting without an answer in flight, the earliest written first:
 * the first unsent one of each chat goes, the rest of the chat follow it one by one.
 */
export function flushOutbox(machine: MachineId): void {
  const mine = [...messagesStore.values()].filter((m) => m.machineId === machine).sort(byMaking);
  for (const m of mine) {
    // A live machine starts the steps over.
    forgetResend(m.clientId);
    resend(m.clientId);
  }
}

/**
 * Resends a machine's unanswered messages each time its socket becomes live, and asks the runtime where the late ones
 * are (commands/message-status); stopping it stops their timers too.
 */
export function startOutbox(): () => void {
  const stopStatus = startMessageStatus();
  const wasLive = new Map<MachineId, boolean>();
  // A message that left the line by an event (the runtime showed it, the status said where it is) lets the next one go.
  const unsubscribeLine = messagesStore.subscribeAll((changed) => {
    for (const id of changed) {
      const m = messagesStore.get(id);
      if (m?.conversationId && !isUnsent(m)) advance(m.machineId, m.conversationId);
      // An event ended a message whose cancel was still unanswered.
      releaseCancel(id);
    }
  });
  const unsubscribe = connectionStore.subscribeAll((changed) => {
    for (const m of changed) {
      const live = connectionStore.get(m)?.k === 'live';
      if (live && !wasLive.get(m)) flushOutbox(m);
      wasLive.set(m, live);
    }
  });
  // The outbox may start after the sockets (me answered late): a machine already live gets its flush now.
  for (const m of [...connectionStore.keys()]) {
    if (connectionStore.get(m)?.k !== 'live' || wasLive.get(m)) continue;
    wasLive.set(m, true);
    flushOutbox(m);
  }
  return () => {
    unsubscribe();
    unsubscribeLine();
    stopStatus();
    for (const id of [...resendTimers.keys()]) forgetResend(id);
  };
}

export const isInFlight = (clientId: ClientId): boolean => inFlight.has(clientId);
