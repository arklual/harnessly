// An agent asking a person (internal/asks, cmd/runtime/routes_asks.go) as the page reads and answers it, and the
// person's settings of it. The runtime never sends a chat's id or a bot's token: an ask names its bot and chat by title.
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const ASK_KIND = { question: 'question', approval: 'approval' } as const;
export type TAskKind = (typeof ASK_KIND)[keyof typeof ASK_KIND];

export const ASK_STATUS = {
  waiting: 'waiting',
  answered: 'answered',
  timeout: 'timeout',
  cancelled: 'cancelled',
  failed: 'failed',
} as const;
export type TAskStatus = (typeof ASK_STATUS)[keyof typeof ASK_STATUS];

export const ASK_DECISION = {
  approved: 'approved',
  changes: 'changes',
  comment: 'comment',
  none: '',
} as const;
export type TAskDecision = (typeof ASK_DECISION)[keyof typeof ASK_DECISION];

/** The reasons the runtime refuses with, each a key of next.asks.reason.*; anything else is shown by its code. */
export const ASK_REASONS = [
  'disabled',
  'no-bot',
  'bot-not-found',
  'unknown-person',
  'bad-person',
  'empty',
  'too-long',
  'rate-limited',
  'not-found',
  'closed',
  'bad-choice',
  'bad-settings',
  'chat-gone',
  'invalid-token',
  'unreachable',
  'rejected',
  'failed',
] as const;

export interface IAskPerson {
  name: string;
  username: string;
}

export interface IAskReply {
  from: IAskPerson;
  text: string;
  /** The button pressed: a decision or an option's text; '' for words. */
  choice: string;
  via: 'telegram' | 'web';
  at: number;
}

export interface IAsk {
  id: string;
  kind: TAskKind;
  status: TAskStatus;
  question: string;
  details: string;
  link: string;
  options: readonly string[];
  /** null: the whole chat was asked. */
  to: IAskPerson | null;
  bot: string;
  chat: string;
  /** Sent without a sound (the quiet hours). */
  silent: boolean;
  /** The conversation of the agent that asks ('' when unknown) and its title. */
  conversation: string;
  agent: string;
  createdAt: number;
  deadline: number;
  closedAt: number;
  decision: TAskDecision;
  answer: IAskReply | null;
  replies: readonly IAskReply[];
  /** When the agent took the outcome, and how many replies it had read then. */
  seenAt: number;
  seenReplies: number;
  error: string;
}

export interface IAskContact {
  id: string;
  name: string;
  username: string;
  telegramId: number;
  me: boolean;
  canApprove: boolean;
  note: string;
}

export interface IAskSettings {
  enabled: boolean;
  defaultBot: string;
  bots: readonly string[];
  onlyContacts: boolean;
  contacts: readonly IAskContact[];
  defaultTimeoutMin: number;
  maxTimeoutMin: number;
  perHour: number;
  quiet: { enabled: boolean; from: string; to: string };
  showSource: boolean;
  browserNotify: boolean;
}

export interface IAskBot {
  id: string;
  name: string;
  chat: string;
  bound: boolean;
}

export interface IAskSettingsView {
  settings: IAskSettings;
  bots: readonly IAskBot[];
}

/** A refusal of the runtime: its reason and detail (a settings field). */
export interface IAskRefusal {
  refused: string;
  detail: string;
}

export type TAskAnswer<T> = T | IAskRefusal;
export type TAskOutcome<T> = PageOutcome<TAskAnswer<T>>;

export const isAskRefusal = <T>(v: TAskAnswer<T>): v is IAskRefusal =>
  typeof v === 'object' && v !== null && 'refused' in v;

const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`asks: ${what}`);
  return v as Record<string, unknown>;
};
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const num = (v: unknown): number => (typeof v === 'number' ? v : 0);
const pick = <T extends string>(all: Record<string, T>, v: unknown, what: string): T => {
  const found = Object.values(all).find((x) => x === v);
  if (found === undefined) throw new Error(`asks: ${what}`);
  return found;
};

const readPerson = (v: unknown): IAskPerson => {
  const o = obj(v, 'person');
  return { name: str(o.name), username: str(o.username) };
};

const readReply = (v: unknown): IAskReply => {
  const o = obj(v, 'reply');
  return {
    from: readPerson(o.from),
    text: str(o.text),
    choice: str(o.choice),
    via: o.via === 'web' ? 'web' : 'telegram',
    at: num(o.at),
  };
};

export const readAsk = (v: unknown): IAsk => {
  const o = obj(v, 'ask');
  const decision = Object.values(ASK_DECISION).find((x) => x === o.decision) ?? ASK_DECISION.none;
  return {
    id: str(o.id),
    kind: pick(ASK_KIND, o.kind, 'kind'),
    status: pick(ASK_STATUS, o.status, 'status'),
    question: str(o.question),
    details: str(o.details),
    link: str(o.link),
    options: Array.isArray(o.options) ? o.options.map(str) : [],
    to: o.to === null || o.to === undefined ? null : readPerson(o.to),
    bot: str(o.bot),
    chat: str(o.chat),
    silent: o.silent === true,
    conversation: str(o.conversation),
    agent: str(o.agent),
    createdAt: num(o.createdAt),
    deadline: num(o.deadline),
    closedAt: num(o.closedAt),
    decision,
    answer: o.answer === null || o.answer === undefined ? null : readReply(o.answer),
    replies: Array.isArray(o.replies) ? o.replies.map(readReply) : [],
    seenAt: num(o.seenAt),
    seenReplies: num(o.seenReplies),
    error: str(o.error),
  };
};

const readContact = (v: unknown): IAskContact => {
  const o = obj(v, 'contact');
  return {
    id: str(o.id),
    name: str(o.name),
    username: str(o.username),
    telegramId: num(o.telegramId),
    me: o.me === true,
    canApprove: o.canApprove === true,
    note: str(o.note),
  };
};

export const readSettingsView = (o: Record<string, unknown>): IAskSettingsView => {
  const s = obj(o.settings, 'settings');
  const quiet = obj(s.quiet, 'quiet');
  const bots = Array.isArray(o.bots) ? o.bots : [];
  return {
    settings: {
      enabled: s.enabled === true,
      defaultBot: str(s.defaultBot),
      bots: Array.isArray(s.bots) ? s.bots.map(str) : [],
      onlyContacts: s.onlyContacts === true,
      contacts: Array.isArray(s.contacts) ? s.contacts.map(readContact) : [],
      defaultTimeoutMin: num(s.defaultTimeoutMin),
      maxTimeoutMin: num(s.maxTimeoutMin),
      perHour: num(s.perHour),
      quiet: { enabled: quiet.enabled === true, from: str(quiet.from), to: str(quiet.to) },
      showSource: s.showSource === true,
      browserNotify: s.browserNotify === true,
    },
    bots: bots.map((b) => {
      const x = obj(b, 'bot');
      return { id: str(x.id), name: str(x.name), chat: str(x.chat), bound: x.bound === true };
    }),
  };
};

const orRefusal =
  <T>(read: (o: Record<string, unknown>) => T) =>
  (v: unknown): TAskAnswer<T> => {
    const o = obj(v, 'answer');
    if (typeof o.error === 'string') return { refused: o.error, detail: str(o.detail) };
    return read(o);
  };

const readList = orRefusal((o) => {
  if (!Array.isArray(o.asks)) throw new Error('asks: asks');
  return o.asks.map(readAsk);
});
const readOne = orRefusal((o) => readAsk(o.ask));
const readSettings = orRefusal(readSettingsView);

const asksPath = (machine: MachineId, rest = '') => runtimePath(platformPath(`/asks${rest}`), machine);
const one = (id: string, action: string) => `/${encodeURIComponent(id)}${action}`;

export const loadAsks = (machine: MachineId): Promise<TAskOutcome<IAsk[]>> =>
  getPage(asksPath(machine), readList, TIMEOUT_MS);

/** The person's answer from the page: a choice (approved, changes, an option's index as o<n>), words, or both. */
export const answerAsk = (
  machine: MachineId,
  id: string,
  answer: { choice?: string; text?: string },
): Promise<TAskOutcome<IAsk>> =>
  sendBody('POST', asksPath(machine, one(id, '/answer')), answer, readOne, TIMEOUT_MS);

export const cancelAsk = (machine: MachineId, id: string): Promise<TAskOutcome<IAsk>> =>
  sendPage('POST', asksPath(machine, one(id, '/cancel')), readOne, TIMEOUT_MS);

const settingsPath = (machine: MachineId) => runtimePath(platformPath('/asks-settings'), machine);

export const loadAskSettings = (machine: MachineId): Promise<TAskOutcome<IAskSettingsView>> =>
  getPage(settingsPath(machine), readSettings, TIMEOUT_MS);

export const saveAskSettings = (
  machine: MachineId,
  settings: IAskSettings,
): Promise<TAskOutcome<IAskSettingsView>> =>
  sendBody('PUT', settingsPath(machine), settings, readSettings, TIMEOUT_MS);
