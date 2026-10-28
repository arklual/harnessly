// Outgoing messages by clientId (MessageStore). A message starts on the page (draft, uploading, submitting); once the
// runtime has it, its state is the runtime's — prompt_state and message_upsert by rev — and the page derives nothing.
// Every state that waits has a term: the runtime's wait for its states, the page's own for submitting.

import { machineNow } from '@/next/core/clock';
import { createKeyedStore, sameItems, useAll, useKey } from '@/next/core/store';
import {
  RETRYABLE_REFUSE_CODES,
  type AgentStateKind,
  waitPhase,
  type AttachmentRef,
  type AutoContinueWhy,
  type TerminalAside,
  type ClientId,
  type ConvId,
  type LocalMessageState,
  type MachineId,
  type Message,
  type MessageKind,
  type MessageRef,
  type MessageMode,
  type MessageState,
  type Ms,
  type RefuseCode,
  type Wait,
} from '@/next/protocol/v2';

export interface MessageView {
  clientId: ClientId;
  machineId: MachineId;
  /** null while conversation.create with this first message has not answered. */
  conversationId: ConvId | null;
  text: string;
  attachments: AttachmentRef[];
  /** The places it points at; once the runtime has it, `text` ends with their block (features/chat/attached.ts). */
  refs?: MessageRef[];
  mode: MessageMode;
  kind: MessageKind;
  /** Set on a message the runtime wrote itself to go on (after a limit's reset, after a restart); absent on the person's. */
  auto?: AutoContinueWhy;
  /** The screen of the agent's terminal closed before the message went in (the person's /login, a picker). */
  cleared?: TerminalAside;
  /**
   * Written while the agent was busy (the page's own fact, not on the wire): the message stands in the stack over the
   * field from the first moment until the agent has it, never in the feed on the way.
   */
  held: boolean;
  createdAt: Ms;
  attempts: number;
  state: LocalMessageState;
  /** The runtime's rev of the message; 0: the runtime has not shown it yet. */
  rev: number;
  /** When the page saw the message enter its state (the machine's clock): the «takes longer» timer counts from it. */
  stateAt: Ms;
  /** The code behind failed{refused}, for the reason under the bubble. */
  refused: RefuseCode | null;
  /** submitting only: the page's term, by the machine's clock (slowMs, cmdTimeoutMs). */
  wait: Wait | null;
  /** submitting only: why the last try did not get an answer; the outbox sends again when the machine is back. */
  issue: 'timeout' | 'network' | 'http' | RefuseCode | null;
}

export const messagesStore = createKeyedStore<ClientId, MessageView>();
/** A conversation's messages in the order they were made. */
export const convMessagesStore = createKeyedStore<ConvId, ClientId[]>();

export const useMessage = (id: ClientId | null) => useKey(messagesStore, id);
export const useConversationMessages = (conv: ConvId | null): ClientId[] =>
  useKey(convMessagesStore, conv ?? null) ?? NONE;
const NONE: ClientId[] = [];

const TERMINAL = new Set<LocalMessageState['k']>(['done', 'cancelled']);
const LOCAL = new Set<LocalMessageState['k']>(['draft', 'uploading', 'submitting']);

export const isTerminal = (s: LocalMessageState): boolean => TERMINAL.has(s.k);
export const isLocalOnly = (m: MessageView): boolean => m.rev === 0;

/** What holds a message back by the agent's state; an idle or ready agent takes it at once. */
export const HOLDS = new Set<AgentStateKind>(['working', 'waiting-permission']);

/** The states in which the message has left the stack: the agent holds it (its entry is the feed's) or it is over. */
const GONE_FROM_STACK = new Set<LocalMessageState['k']>(['accepted', 'in-turn', 'done', 'cancelled']);

/**
 * The message stands in the stack over the field, one place from the first moment to the moment the agent has it:
 * it was written while the agent was busy (held), something holds it back — the queue is paused, a menu is up, another
 * message is ahead, the agent works — or it is typed into a working agent (steer). A message typed into an agent that
 * takes it is going out, and the feed shows it at its end until it is the prompt of its turn: it does not drop into
 * the stack and come back.
 */
export const waitsInQueue = (m: MessageView, agent: AgentStateKind | undefined): boolean => {
  const { state } = m;
  // The runtime's own «go on» is not the person's to move or take back: it stands in the feed as a quiet line.
  if (m.auto) return false;
  if (GONE_FROM_STACK.has(state.k)) return false;
  if (m.held) return true;
  if (state.k === 'queued') {
    const { position, reason } = state;
    if (
      position > 1 ||
      reason === 'paused' ||
      reason === 'blocked-menu' ||
      reason === 'blocked-text' ||
      reason === 'stalled'
    )
      return true;
    return reason !== 'launching' && reason !== 'resuming' && agent !== undefined && HOLDS.has(agent);
  }
  return m.mode === 'steer' && state.k !== 'failed';
};

/** The messages not yet in a turn and not terminal: the queue strip takes those that wait (waitsInQueue). */
export function usePendingMessages(conv: ConvId | null): MessageView[] {
  return useAll(
    messagesStore,
    (s) =>
      (conv ? (convMessagesStore.get(conv) ?? []) : [])
        .map((id) => s.get(id))
        .filter((m): m is MessageView => !!m && !['in-turn', 'done', 'cancelled'].includes(m.state.k)),
    sameItems,
  );
}

/** The wait of whatever state the message is in, by the machine's clock; null: nothing to wait on (agent-busy). */
export function waitOf(m: MessageView): Wait | null {
  const s = m.state;
  if (s.k === 'submitting') return m.wait;
  if (s.k === 'queued') return s.wait;
  if (s.k === 'typing' || s.k === 'in-input') return s.wait;
  return null;
}

/** normal, slow («дольше обычного») or overdue (offer retry or cancel) at a moment of the machine's clock. */
export function messagePhase(m: MessageView, machineNow: Ms): 'normal' | 'slow' | 'overdue' {
  const w = waitOf(m);
  return w ? waitPhase(w, machineNow) : 'normal';
}

// ---- the page's own transitions; the runtime's states come only through applyServer*

/** Which page-side moves are allowed; the runtime's states are taken as they come, by rev. */
const LOCAL_MOVES: Record<'draft' | 'uploading' | 'submitting', LocalMessageState['k'][]> = {
  draft: ['uploading', 'submitting'],
  uploading: ['submitting', 'failed'],
  submitting: ['submitting', 'failed', 'cancelled'],
};

export function canMoveLocally(from: LocalMessageState, to: LocalMessageState): boolean {
  if (from.k === 'failed') return to.k === 'submitting' || to.k === 'cancelled';
  if (!LOCAL.has(from.k)) return false;
  return LOCAL_MOVES[from.k as keyof typeof LOCAL_MOVES].includes(to.k);
}

export function putLocal(m: MessageView): void {
  messagesStore.set(m.clientId, m);
  if (m.conversationId) addToConversation(m.conversationId, m.clientId);
}

function addToConversation(conv: ConvId, id: ClientId): void {
  convMessagesStore.patch(conv, (list = []) => (list.includes(id) ? list : [...list, id]));
}

/** A page-side move (submitting, failed{refused}, failed{upload-failed}, cancelled of a message the runtime never had). */
export function moveLocally(
  id: ClientId,
  fn: (m: MessageView) => Partial<MessageView> & { state: LocalMessageState },
): boolean {
  const m = messagesStore.get(id);
  if (!m || m.rev > 0) return false;
  const next = fn(m);
  if (!canMoveLocally(m.state, next.state)) return false;
  messagesStore.set(id, {
    ...m,
    ...next,
    stateAt: m.state.k === next.state.k ? m.stateAt : machineNow(m.machineId),
  });
  return true;
}

export function setConversationOf(id: ClientId, conv: ConvId): void {
  messagesStore.patch(id, (m) => (m && m.conversationId !== conv ? { ...m, conversationId: conv } : m));
  addToConversation(conv, id);
}

/** prompt_state: the runtime moved a message; older or equal revs change nothing. */
export function applyPromptState(
  conv: ConvId,
  machine: MachineId,
  id: ClientId,
  state: MessageState,
  rev: number,
): void {
  const m = messagesStore.get(id);
  if (!m) {
    // Sent from another device or before this page: its text comes with message_upsert or the snapshot.
    return;
  }
  // A message is its machine's: another machine's frame with the same clientId moves nothing.
  if (m.machineId !== machine || m.rev >= rev) return;
  messagesStore.set(id, {
    ...m,
    machineId: machine,
    conversationId: conv,
    state,
    stateAt: m.state.k === state.k ? m.stateAt : machineNow(machine),
    rev,
    wait: null,
    issue: null,
    refused: null,
  });
  addToConversation(conv, id);
}

/** message_upsert or a snapshot's messages: the whole record. */
export function applyMessage(machine: MachineId, msg: Message): void {
  const m = messagesStore.get(msg.clientId);
  if (m && (m.machineId !== machine || m.rev >= msg.rev)) return;
  messagesStore.set(msg.clientId, {
    clientId: msg.clientId,
    machineId: machine,
    conversationId: msg.conversationId,
    text: msg.text,
    attachments: msg.attachments,
    refs: msg.refs ?? m?.refs,
    mode: msg.mode,
    kind: msg.kind,
    ...(msg.auto && { auto: msg.auto }),
    ...(msg.cleared && { cleared: msg.cleared }),
    held: m?.held ?? false,
    createdAt: msg.createdAt,
    attempts: msg.attempts,
    state: msg.state,
    stateAt: m && m.state.k === msg.state.k ? m.stateAt : machineNow(machine),
    rev: msg.rev,
    refused: null,
    wait: null,
    issue: null,
  });
  addToConversation(msg.conversationId, msg.clientId);
}

/**
 * The refusals whose cause may have passed by the time the person looks again (the agent was gone during an incident, the
 * machine was away, the answer did not come): the words can be sent again as a new message. The rest — too long, a bad
 * attachment, no rights — would get the same no, so they are only deleted.
 */
export const PASSING_REFUSE_CODES = ['agent-gone', 'machine-offline', 'timeout'] as const;

export const isPassingRefusal = (code: RefuseCode | null): boolean =>
  code !== null && (PASSING_REFUSE_CODES as readonly string[]).includes(code);

export const isRetryableRefusal = (code: RefuseCode): boolean =>
  (RETRYABLE_REFUSE_CODES as readonly string[]).includes(code);

/** Forgets the messages of a conversation the page let go of, except the ones still on their way. */
export function dropConversationMessages(conv: ConvId): void {
  const ids = convMessagesStore.get(conv) ?? [];
  const keep = ids.filter((id) => {
    const m = messagesStore.get(id);
    const settled = !m || isTerminal(m.state) || m.state.k === 'in-turn';
    if (settled) messagesStore.delete(id);
    return !settled;
  });
  if (keep.length) convMessagesStore.set(conv, keep);
  else convMessagesStore.delete(conv);
}

/**
 * A conversation snapshot lists every message of the runtime that is not over, and the finished ones of the last minutes
 * only (outbox.FinalKept). A message the runtime had (rev > 0) that the page still holds as not over and that the snapshot
 * no longer lists has ended while the page was away — a chat kept in the browser through a sleep or a reload that came
 * back after its messages were answered. It leaves the page; otherwise it would stand in the queue over the field for
 * ever, since no later frame ever names it again.
 */
export function forgetEndedElsewhere(conv: ConvId, machine: MachineId, listed: ReadonlySet<ClientId>): void {
  const ids = convMessagesStore.get(conv) ?? [];
  const keep = ids.filter((id) => {
    const m = messagesStore.get(id);
    const ended =
      !!m &&
      m.machineId === machine &&
      m.conversationId === conv &&
      m.rev > 0 &&
      !isTerminal(m.state) &&
      !listed.has(id);
    if (ended) messagesStore.delete(id);
    return !ended;
  });
  if (keep.length !== ids.length) convMessagesStore.set(conv, keep);
}

/** The agent did not take it in time: back at the head of the queue, waiting for the person's Повторить. */
export const isStalled = (m: MessageView): boolean => m.state.k === 'queued' && m.state.reason === 'stalled';

/**
 * A message the runtime says it does not know (the status of its clientId is unknown): nothing of it is on the
 * runtime, so the page's copy is all there is. It goes back to the queue over the field as the page's own, not sent,
 * with Повторить — it is never lost, and never sent again by itself (the runtime is there and did not get it, so
 * something is wrong beyond the road).
 */
export function returnToQueue(id: ClientId, at: Ms): void {
  messagesStore.patch(id, (m) =>
    m
      ? {
          ...m,
          held: true,
          rev: 0,
          state: { k: 'failed', code: 'unconfirmed', retryable: true, at },
          stateAt: at,
          wait: null,
          issue: null,
          refused: null,
        }
      : m,
  );
}

/**
 * A message of the runtime it no longer has (refused not-found): it ended long enough ago that the runtime let its record
 * go. The page's copy is the only thing left of it, and it goes.
 */
export function forgetMessage(id: ClientId): void {
  const m = messagesStore.get(id);
  if (!m) return;
  messagesStore.delete(id);
  if (m.conversationId)
    convMessagesStore.patch(m.conversationId, (list = []) => list.filter((x) => x !== id));
}
