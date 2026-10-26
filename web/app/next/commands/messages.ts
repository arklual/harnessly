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

import { machineNow, pageClock } from '@/next/core/clock';
import { uuidv7 } from '@/next/core/ids';
import { batch } from '@/next/core/store';
import { startMessageStatus } from '@/next/commands/message-status';
import { runCommand, type OpHandle } from '@/next/commands/run';
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

const inFlight = new Set<ClientId>();

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
  pageClock().clearTimeout(resendTimers.get(clientId));
  const n = resendCount.get(clientId) ?? 0;
  resendCount.set(clientId, n + 1);
  resendTimers.set(
    clientId,
    pageClock().setTimeout(
      () => {
        resendTimers.delete(clientId);
        resend(clientId);
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
  putLocal(newView(machine, clientId, out.conversationId, out));
  void submit(clientId);
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

/** Sends (or sends again) a message the runtime has not shown yet. */
async function submit(clientId: ClientId): Promise<void> {
  const m = messagesStore.get(clientId);
  if (!m || m.rev > 0 || !m.conversationId || inFlight.has(clientId)) return;
  inFlight.add(clientId);
  try {
    const handle = runCommand(
      m.machineId,
      'send',
      {
        clientId,
        conversationId: m.conversationId,
        text: m.text,
        attachmentIds: m.attachments.map((a) => a.id),
        mode: m.mode,
        kind: m.kind,
        ...(m.refs && m.refs.length > 0 ? { refs: m.refs } : {}),
      },
      clientId,
    );
    settleSubmit(clientId, await handle.done);
  } finally {
    inFlight.delete(clientId);
  }
}

/** The answer to send (or to conversation.create with this first message) as the message's page state. */
export function settleSubmit(clientId: ClientId, st: Awaited<OpHandle['done']>): void {
  if (st.k === 'done') {
    // The runtime has it; what it does next comes as its event. The page stops saying it waits for the machine.
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
  if (m.rev === 0) {
    if (m.state.k === 'failed' && !m.state.retryable) return null;
    if (m.state.k === 'failed' && m.state.code === 'unconfirmed' && m.conversationId) {
      // The runtime said it does not know this clientId, yet its memory of the opId answers it: the words go again as
      // a new message (nothing of the old one is on the runtime, so it can never be typed twice), in the same place.
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
    forgetResend(clientId);
    moveLocally(clientId, (v) => ({
      state: { k: 'submitting' },
      wait: submitWait(v.machineId),
      issue: null,
      refused: null,
      attempts: v.attempts + 1,
    }));
    void submit(clientId);
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

/** Отменить. A message the runtime refused is only the page's; one it may have is cancelled there. */
export function cancelMessage(clientId: ClientId): OpHandle | null {
  const m = messagesStore.get(clientId);
  if (!m || !m.conversationId) return null;
  // Nothing of it is on the runtime — it refused it, or no try has been answered and none is on its way — so the
  // page's copy is all there is, and it goes at once, machine or no machine.
  if (m.rev === 0 && (m.state.k === 'failed' || (m.state.k === 'submitting' && !inFlight.has(clientId)))) {
    const unanswered = m.state.k === 'submitting';
    forgetResend(clientId);
    moveLocally(clientId, () => ({ state: { k: 'cancelled', by: 'user' }, wait: null, issue: null }));
    // An earlier try may have reached the runtime without its answer reaching the page: tell it, if it can hear. The
    // person's Delete is done either way, so the answer of this one is nobody's to wait for.
    if (unanswered) void runCommand(m.machineId, 'cancel', { conversationId: m.conversationId, clientId });
    return null;
  }
  const conv = m.conversationId;
  return forgetIfUnknown(
    clientId,
    conv,
    runCommand(m.machineId, 'cancel', { conversationId: conv, clientId }),
  );
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

/** Sends an unanswered message again, keeping what the page said of the last try until the new one is answered. */
function resend(clientId: ClientId): void {
  const m = messagesStore.get(clientId);
  if (!m || m.rev !== 0 || m.state.k !== 'submitting' || inFlight.has(clientId)) return;
  batch(() => moveLocally(clientId, (v) => ({ state: { k: 'submitting' }, wait: submitWait(v.machineId) })));
  void submit(clientId);
}

/** Sends again every message of machine that is submitting without an answer in flight. */
export function flushOutbox(machine: MachineId): void {
  for (const m of [...messagesStore.values()]) {
    if (m.machineId !== machine) continue;
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
  const unsubscribe = connectionStore.subscribeAll((changed) => {
    for (const m of changed) {
      const live = connectionStore.get(m)?.k === 'live';
      if (live && !wasLive.get(m)) flushOutbox(m);
      wasLive.set(m, live);
    }
  });
  return () => {
    unsubscribe();
    stopStatus();
    for (const id of [...resendTimers.keys()]) forgetResend(id);
  };
}

export const isInFlight = (clientId: ClientId): boolean => inFlight.has(clientId);
