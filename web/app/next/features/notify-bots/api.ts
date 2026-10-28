// The Telegram bots for notifications (internal/notifybots, cmd/runtime/routes_notifybots.go) as the page reads and
// changes them. The runtime never sends a token back: a bot says only that one is set and its last characters.
import { getPage, sendBody, sendPage, type PageOutcome } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const LIST_TIMEOUT_MS = 10_000;
/** A change that asks Telegram (getMe, sendMessage) waits for its answer too. */
const TELEGRAM_TIMEOUT_MS = 25_000;

/** The reasons the runtime refuses with; anything else is shown by its code. */
export const NOTIFY_REASONS = [
  'bad-token',
  'invalid-token',
  'duplicate',
  'not-found',
  'not-bound',
  'webhook',
  'chat-gone',
  'rate-limited',
  'unreachable',
  'rejected',
  'bad-name',
  'bad-instruction',
  'expired',
  'failed',
  'not-linked',
  'no-shared',
  'offline',
  'disabled',
] as const;
export type TNotifyReason = (typeof NOTIFY_REASONS)[number];

export const BINDING_STATES = ['waiting', 'failed'] as const;

export interface INotifyBinding {
  state: (typeof BINDING_STATES)[number];
  code: string;
  /** Opens the bot with the code (a private chat). */
  link: string;
  /** What to send in a group the bot was added to. */
  command: string;
  expiresAt: number;
  reason: string;
}

export interface INotifyBot {
  id: string;
  name: string;
  username: string;
  title: string;
  tokenSet: boolean;
  tokenTail: string;
  chat: { title: string; type: string } | null;
  instruction: string;
  binding: INotifyBinding | null;
}

/** A refusal of the runtime: its reason, and Telegram's words when it gave some. */
export interface INotifyRefusal {
  refused: string;
  detail: string;
}

export type TNotifyAnswer<T> = T | INotifyRefusal;

export const isRefusal = <T>(v: TNotifyAnswer<T>): v is INotifyRefusal =>
  typeof v === 'object' && v !== null && 'refused' in v;

const obj = (v: unknown, what: string): Record<string, unknown> => {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) throw new Error(`notify-bots: ${what}`);
  return v as Record<string, unknown>;
};
const str = (v: unknown, what: string): string => {
  if (typeof v !== 'string') throw new Error(`notify-bots: ${what}`);
  return v;
};
const num = (v: unknown, what: string): number => {
  if (typeof v !== 'number') throw new Error(`notify-bots: ${what}`);
  return v;
};

const readBinding = (v: unknown): INotifyBinding | null => {
  if (v === null || v === undefined) return null;
  const o = obj(v, 'binding');
  const state = BINDING_STATES.find((s) => s === o.state);
  if (state === undefined) throw new Error('notify-bots: binding state');
  return {
    state,
    code: str(o.code, 'code'),
    link: str(o.link, 'link'),
    command: str(o.command, 'command'),
    expiresAt: num(o.expiresAt, 'expiresAt'),
    reason: str(o.reason, 'reason'),
  };
};

export const readBot = (v: unknown): INotifyBot => {
  const o = obj(v, 'bot');
  let chat: INotifyBot['chat'] = null;
  if (o.chat !== null && o.chat !== undefined) {
    const c = obj(o.chat, 'chat');
    chat = { title: str(c.title, 'chat title'), type: str(c.type, 'chat type') };
  }
  return {
    id: str(o.id, 'id'),
    name: str(o.name, 'name'),
    username: str(o.username, 'username'),
    title: str(o.title, 'title'),
    tokenSet: o.tokenSet === true,
    tokenTail: str(o.tokenTail, 'tokenTail'),
    chat,
    instruction: str(o.instruction, 'instruction'),
    binding: readBinding(o.binding),
  };
};

/** An answer of the runtime: what `read` makes of it, or its refusal ({error, detail?} with a 4xx or 5xx). */
const orRefusal =
  <T>(read: (o: Record<string, unknown>) => T) =>
  (v: unknown): TNotifyAnswer<T> => {
    const o = obj(v, 'answer');
    if (typeof o.error === 'string')
      return { refused: o.error, detail: typeof o.detail === 'string' ? o.detail : '' };
    return read(o);
  };

const readList = orRefusal((o) => {
  if (!Array.isArray(o.bots)) throw new Error('notify-bots: bots');
  return o.bots.map(readBot);
});
const readOne = orRefusal((o) => readBot(o.bot));
const readRemoved = orRefusal((o) => str(o.removed, 'removed'));

const botsPath = (machine: MachineId, rest = '') => runtimePath(platformPath(`/notify-bots${rest}`), machine);
const one = (id: string, action = '') => `/${encodeURIComponent(id)}${action}`;

export type TNotifyOutcome<T> = PageOutcome<TNotifyAnswer<T>>;

export const loadBots = (machine: MachineId): Promise<TNotifyOutcome<INotifyBot[]>> =>
  getPage(botsPath(machine), readList, LIST_TIMEOUT_MS);

/** Links a bot by its token (the runtime asks Telegram whose it is). */
export const addBot = (machine: MachineId, token: string): Promise<TNotifyOutcome<INotifyBot>> =>
  sendBody('POST', botsPath(machine), { token }, readOne, TELEGRAM_TIMEOUT_MS);

export interface INotifyChange {
  name?: string;
  instruction?: string;
  token?: string;
}

export const editBot = (
  machine: MachineId,
  id: string,
  change: INotifyChange,
): Promise<TNotifyOutcome<INotifyBot>> =>
  sendBody('PUT', botsPath(machine, one(id)), change, readOne, TELEGRAM_TIMEOUT_MS);

export const removeBot = (machine: MachineId, id: string): Promise<TNotifyOutcome<string>> =>
  sendPage('DELETE', botsPath(machine, one(id)), readRemoved, LIST_TIMEOUT_MS);

/** A new one-time code; the runtime waits for it in the bot's messages (ten minutes). */
export const startBinding = (machine: MachineId, id: string): Promise<TNotifyOutcome<INotifyBot>> =>
  sendPage('POST', botsPath(machine, one(id, '/bind')), readOne, LIST_TIMEOUT_MS);

export const stopBinding = (machine: MachineId, id: string): Promise<TNotifyOutcome<INotifyBot>> =>
  sendPage('DELETE', botsPath(machine, one(id, '/bind')), readOne, LIST_TIMEOUT_MS);

export const testBot = (machine: MachineId, id: string): Promise<TNotifyOutcome<INotifyBot>> =>
  sendPage('POST', botsPath(machine, one(id, '/test')), readOne, TELEGRAM_TIMEOUT_MS);

// The shared bot (internal/notifybots/shared.go): the person's one Telegram, connected with a one-time code
// the bot reads from their Start. The page never sees a token or a chat id, only the chat's title.

export interface ISharedBinding {
  /** Opens the shared bot with the code. */
  link: string;
  expiresAt: number;
}

export interface ISharedState {
  /** There is a shared bot here; with none the page offers the person's own bots. */
  enabled: boolean;
  bot: string;
  chat: { title: string } | null;
  instruction: string;
  binding: ISharedBinding | null;
}

export interface ISharedView {
  shared: ISharedState;
  /** Bots of the person's own are offered too. */
  ownBots: boolean;
  /** Why the state is the one last seen and not fresh ('' when it is fresh). */
  problem: string;
}

export const readShared = (v: unknown): ISharedState => {
  const o = obj(v, 'shared');
  let chat: ISharedState['chat'] = null;
  if (o.chat !== null && o.chat !== undefined) chat = { title: str(obj(o.chat, 'chat').title, 'chat title') };
  let binding: ISharedBinding | null = null;
  if (o.binding !== null && o.binding !== undefined) {
    const b = obj(o.binding, 'binding');
    const link = str(b.link, 'link');
    if (!link.startsWith('https://t.me/')) throw new Error('notify-bots: link');
    binding = { link, expiresAt: num(b.expiresAt, 'expiresAt') };
  }
  return {
    enabled: o.enabled === true,
    bot: str(o.bot, 'bot'),
    chat,
    instruction: str(o.instruction, 'instruction'),
    binding,
  };
};

const readView = orRefusal((o): ISharedView => ({
  shared: readShared(o.shared),
  ownBots: o.ownBots === true,
  problem: typeof o.problem === 'string' ? o.problem : '',
}));

const sharedPath = (machine: MachineId, rest = '') =>
  runtimePath(platformPath(`/notify-shared${rest}`), machine);

/** The cloud may take a few seconds to answer; the runtime gives up on it first and shows what it saw last. */
const SHARED_TIMEOUT_MS = 15_000;

export const loadShared = (machine: MachineId): Promise<TNotifyOutcome<ISharedView>> =>
  getPage(sharedPath(machine), readView, SHARED_TIMEOUT_MS);

/** A new one-time code: the answer's binding links to the bot with it. */
export const linkShared = (machine: MachineId): Promise<TNotifyOutcome<ISharedView>> =>
  sendPage('POST', sharedPath(machine, '/link'), readView, TELEGRAM_TIMEOUT_MS);

export const unlinkShared = (machine: MachineId): Promise<TNotifyOutcome<ISharedView>> =>
  sendPage('DELETE', sharedPath(machine, '/link'), readView, TELEGRAM_TIMEOUT_MS);

export const saveSharedInstruction = (
  machine: MachineId,
  instruction: string,
): Promise<TNotifyOutcome<ISharedView>> =>
  sendBody('PUT', sharedPath(machine), { instruction }, readView, TELEGRAM_TIMEOUT_MS);

export const testShared = (machine: MachineId): Promise<TNotifyOutcome<ISharedView>> =>
  sendPage('POST', sharedPath(machine, '/test'), readView, TELEGRAM_TIMEOUT_MS);
