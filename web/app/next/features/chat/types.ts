// What the conversation view draws, as plain props. The wire types are a copy of the shapes in protocol/v2.ts (the v2
// contract, internal/wire on the Go side) that this view reads; once protocol/v2.ts is on this branch they become
// re-exports from it and nothing here changes shape. The view types below them are what the stores hand the feed.
import type { AutoContinueWhy, Effort, MessageRef, TurnOutput } from '@/next/protocol/v2';

export type { OutputKind, TurnOutput } from '@/next/protocol/v2';

export type Ms = number;
export type EntryId = string;
export type ClientId = string;
export type Provider = 'claude' | 'codex';

// ---- wire: turns and entries

export type TurnStatus = 'live' | 'done' | 'interrupted' | 'failed';
export type TurnFailureCode = 'api-error' | 'rate-limit' | 'context-overflow' | 'agent-exited' | 'unknown';
export type FileOp = 'created' | 'edited' | 'read';

export interface TurnFailure {
  code: TurnFailureCode;
  detail: string | null; // the agent's raw text: data, not a translation
  retryAt: Ms | null;
}

export interface Turn {
  id: EntryId; // the id of the user entry that opened the turn
  author: { provider: Provider; role: string | null };
  startedAt: Ms;
  endedAt: Ms | null;
  durationMs: number | null;
  toolCount: number;
  stepCount: number;
  status: TurnStatus;
  failure: TurnFailure | null;
  answerEntryId: EntryId | null;
  files: { path: string; op: FileOp }[];
  /** What the turn made to open; absent when nothing. */
  outputs?: TurnOutput[];
}

export interface AttachmentRef {
  id: string;
  name: string;
  kind: 'image' | 'file';
  mime: string;
}

export interface ToolOutput {
  text: string;
  truncated: { shown: number; total: number } | null;
}

interface EntryBase {
  id: EntryId;
  turnId: EntryId | null;
  at: Ms;
  rev: number;
}

export type UserEntry = EntryBase & {
  t: 'user';
  text: string;
  attachments: AttachmentRef[];
  clientId: ClientId | null;
};
export type SayEntry = EntryBase & { t: 'say'; md: string };
export type ThinkEntry = EntryBase & { t: 'think'; text: string };
export type ToolEntry = EntryBase & {
  t: 'tool';
  toolUseId: string;
  tool: string;
  summary: string;
  ok: boolean | null; // null: still running
  output: ToolOutput | null;
};
export type SubagentEntry = EntryBase & {
  t: 'subagent';
  agentId: string;
  agentType: string;
  description: string;
  status: 'running' | 'done' | 'failed';
  model: string | null;
  durationMs: number | null;
  output: ToolOutput | null; // what the agent returned, once it has
};
export type AnswersEntry = EntryBase & {
  t: 'answers';
  questionId: string;
  pairs: { q: string; a: string }[];
};
export type GateEntry = EntryBase & { t: 'gate'; permissionId: string };
export type CompactionEntry = EntryBase & {
  t: 'compaction';
  tokensBefore: number | null;
  tokensAfter: number | null;
  keep: string[];
};
export type NoticeEntry = EntryBase & {
  t: 'notice';
  level: 'info' | 'warn' | 'error';
  code: string;
  detail: string | null;
};
export type InterruptEntry = EntryBase & { t: 'interrupt' };
/** A message another agent's session wrote to this one; `md` is its words, drawn as Markdown text. */
export interface PeerNote {
  from: string;
  kind: 'message' | 'done' | 'idle' | 'event';
  color: string | null;
  event: string | null;
  summary: string | null;
  sentAt: Ms | null;
  /** Set when a background subagent of this session wrote it; `from` is then its task ("" when unknown). */
  agentId: string | null;
  model: string | null;
  md: string;
}
export type PeerEntry = EntryBase & { t: 'peer'; peers: PeerNote[] };

export type Entry =
  | UserEntry
  | SayEntry
  | ThinkEntry
  | ToolEntry
  | SubagentEntry
  | AnswersEntry
  | GateEntry
  | CompactionEntry
  | NoticeEntry
  | InterruptEntry
  | PeerEntry;

/** What the agent is doing this moment, off its screen (2 Hz at most). */
export interface Now {
  turnId: EntryId;
  thought: string | null;
  action: { tool: string; summary: string; command: string | null } | null;
  word: string | null;
  at: Ms;
}

// ---- wire: messages

export type MessageMode = 'queue' | 'now' | 'steer';
export type QueueReason =
  'behind' | 'agent-busy' | 'launching' | 'resuming' | 'paused' | 'blocked-menu' | 'blocked-text';
export type MessageFailure =
  | 'upload-failed'
  | 'refused'
  | 'launch-failed'
  | 'agent-gone'
  | 'type-failed'
  | 'stuck-in-input'
  | 'unconfirmed'
  | 'expired'
  | 'returned-to-input';

export type MessageState =
  | { k: 'queued'; position: number; reason: QueueReason }
  | { k: 'typing' }
  | { k: 'in-input' }
  | { k: 'accepted'; entryId: EntryId | null }
  | { k: 'in-turn'; turnId: EntryId }
  | { k: 'done'; turnId: EntryId }
  | { k: 'failed'; code: MessageFailure; retryable: boolean; at: Ms }
  | { k: 'cancelled'; by: 'user' };

/** A message on the page: the wire states and the ones before the runtime has it. */
export type LocalMessageState =
  { k: 'draft' } | { k: 'uploading'; done: number; total: number } | { k: 'submitting' } | MessageState;

export interface Message {
  clientId: ClientId;
  text: string;
  attachments: AttachmentRef[];
  /** The places it points at, while the runtime has not written their block into the text yet. */
  refs?: MessageRef[];
  mode: MessageMode;
  createdAt: Ms;
  /** When the message entered its current state, by the page's clock: the «takes longer» timer counts from it. */
  stateSince: Ms;
  state: LocalMessageState;
  /**
   * Typed into a working agent: it sits in the agent's own queue and is read at the agent's next step, not before.
   * Only the transcript says it was read, so until then the message does not claim it was.
   */
  handed?: boolean;
  /** Submitting, and the last try got no answer from the machine (or the machine said it is not there): it waits. */
  waitsMachine?: boolean;
  /** failed{refused}: the runtime's code (a key of next.refuse.*), for the reason under the bubble. */
  refusal?: string | null;
  /** failed{refused} for a cause that may have passed: the same words can be sent again as a new message. */
  resendable?: boolean;
}

export type PauseReason = 'interrupted' | 'user' | 'launch-failed' | 'agent-gone' | 'limited';

export interface QueuePause {
  reason: PauseReason;
  at: Ms;
}

// ---- view: what the feed lists

/**
 * The runtime went on by itself with this message: the feed draws a quiet line at it. `own`: the runtime wrote the
 * message itself, so the line stands in place of a bubble; otherwise it is the person's queued message and the line
 * stands right above its bubble.
 */
export interface AutoMark {
  why: AutoContinueWhy;
  own: boolean;
}

/** Why a live turn stands still: the machine stopped answering, it waits for it rather than failing. */
export type TurnWait = 'machine' | null;

export interface TurnItem {
  kind: 'turn';
  key: EntryId;
  turn: Turn;
  /** The person's message that opened the turn (null for a turn the agent started itself). */
  prompt: UserEntry | null;
  /** The subagent whose return woke the agent and opened this turn: shown above the work, not folded with it. */
  woke: SubagentEntry | null;
  /** What other agents' sessions wrote, when that opened the turn: shown where the person's message would be. */
  peer: PeerEntry | null;
  /** The harness's note that woke the turn (a goal check-in, a task's notification): a quiet line above the work. */
  note: NoticeEntry | null;
  /** The steps without the answer; null: not loaded yet (older turns arrive with their answer only). */
  steps: Entry[] | null;
  /** The agent's last reply, shown under the folded work (turn.answerEntryId). */
  answer: SayEntry | null;
  now: Now | null;
  wait: TurnWait;
  /** What went with a stop or a failure, e.g. «the question was withdrawn». */
  endNote: string | null;
  /** The prompt is a message the runtime sent to go on by itself; absent for any other turn. */
  auto?: AutoMark;
}

/** A message of the person that has no turn yet: going out, refused, or waiting for the network. */
export interface OutgoingItem {
  kind: 'outgoing';
  key: ClientId;
  message: Message;
  /** The message is one the runtime sends to go on by itself; absent for any other. */
  auto?: AutoMark;
}

/** A model the conversation ran on, as the feed names it. */
export interface SwitchModel {
  id: string;
  effort: Effort | null;
  /** Claude Code's ultracode mode; absent is off. */
  ultracode?: boolean;
}

/** A change of model the runtime applied, drawn where it happened: «Model: Opus 5.5 → Sonnet 5.5 · effort high». */
export interface ModelSwitchItem {
  kind: 'switch';
  key: string;
  at: Ms;
  /** Null: the conversation had no model on record. */
  from: SwitchModel | null;
  to: SwitchModel;
  /** The change the agent did not take (it kept its model, or the model has no such mode); absent when it took. */
  refused?: string;
}

/**
 * The turns a rewind left behind, folded into one line where they were: «Откатено 3 хода». It opens to show them
 * as they were (read only); the last rewind can be taken back for a few seconds.
 */
export interface RewoundItem {
  kind: 'rewound';
  key: string;
  /** Which of the conversation's rewinds it is (oldest first): the page of its turns is asked by it. */
  n: number;
  at: Ms;
  turns: number;
  /** The folder went back too. */
  files: boolean;
  /** Until when, by the page's clock, «Вернуть» takes the rewind back; null: it cannot (not the last one). */
  undoUntil: Ms | null;
}

/**
 * Where the conversation came from, drawn where it starts being its own: after the turn a fork branched at, or above
 * all for a handoff. `from` and `to` are the models as people say them; null when not on record.
 */
export interface OriginItem {
  kind: 'origin';
  key: string;
  at: Ms;
  fork: boolean;
  fromId: string;
  title: string;
  from: string | null;
  to: string | null;
}

export type FeedItem = TurnItem | OutgoingItem | ModelSwitchItem | RewoundItem | OriginItem;

/** A reference the agent's text mentions: a task, a merge request, a file or a link. */
export type RefKind = 'task' | 'mr' | 'file' | 'link';

export interface RefInfo {
  kind: RefKind;
  /** What the chip shows. */
  label: string;
  /** The card on hover; null: only the label is known. */
  title: string | null;
  status: string | null;
  tone: 'accent' | 'warn' | 'bad' | null;
  meta: string | null;
}

export type FeedDensity = 'compact' | 'full';
