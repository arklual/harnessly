// The right pane of each chat, as the person left it: whether it is out and how wide, its tabs in their
// order, the one in front and where each tab is (a browser tab's history, the folder of the files tab, the design open
// in Designly and the page in it, a shell's terminal). Going to another chat and back finds the pane as it was. This
// browser's only: localStorage, read once, written a moment after a change; the most recent chats are kept, the ones
// the machine no longer lists are forgotten. A request for a chat that is not on screen waits in its entry and is
// placed when the person comes back, instead of taking over the chat they are in.
import { useCallback, useState, useSyncExternalStore } from 'react';
import type { IPaneTabs, TTab } from '@/next/features/pane/pane-tabs';
import type { IPaneRequest } from '@/next/features/pane/intents';

export interface IChatPane extends IPaneTabs {
  open: boolean;
  /** The pane takes the whole stage. */
  maxed: boolean;
  /** The width dragged in this chat; null: the device's. */
  width: number | null;
  /** What the views of the pane asked to keep (useChatPaneView), by their key. */
  view: Readonly<Record<string, unknown>>;
  /** Asked while the chat was not on screen; placed, oldest first, when it is. */
  pending: readonly IPaneRequest[];
}

export interface IKeptPane extends IChatPane {
  /** The machine of the chat: its list says whether the chat is still there. */
  machine: string | null;
  /** When it was last changed or looked at: the oldest go first. */
  at: number;
}

export type TKept = ReadonlyMap<string, IKeptPane>;

export const PANE_STATE_KEY = 'hx-pane-chats';
const VERSION = 1;
/** More chats than this are not a working set; the least recent are forgotten. */
export const MAX_CHATS = 50;
/** What waits for a chat beyond this is older than anything worth showing. */
export const MAX_PENDING = 10;
const PERSIST_MS = 300;

/** A chat nothing was kept for: the pane closed, no tabs, the device's width. */
export const EMPTY_PANE: IChatPane = Object.freeze({
  open: false,
  maxed: false,
  width: null,
  tabs: [],
  active: '',
  unread: [],
  view: {},
  pending: [],
});

// ── Reading what was kept ──

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const isString = (v: unknown): v is string => typeof v === 'string';
const stringOrNull = (v: unknown): v is string | null => v === null || typeof v === 'string';

const isAddress = (v: unknown): boolean => {
  if (!isRecord(v) || !isString(v.text) || !isString(v.path) || !isRecord(v.target)) return false;
  const target = v.target;
  if (target.kind === 'port') return typeof target.port === 'number';
  return target.kind === 'url' && isString(target.origin);
};

const SIMPLE_KINDS: ReadonlySet<string> = new Set(['side', 'plan', 'machine']);

const isPaneTab = (v: unknown): v is TTab => {
  if (!isRecord(v) || !isString(v.id) || !isString(v.kind)) return false;
  if (SIMPLE_KINDS.has(v.kind)) return true;
  switch (v.kind) {
    case 'designly':
      return stringOrNull(v.design);
    case 'subagent':
      return stringOrNull(v.focus);
    case 'files':
      return isString(v.path) && typeof v.file === 'boolean';
    case 'shell':
      return isString(v.terminal);
    case 'browser':
      return (
        Array.isArray(v.hist) &&
        v.hist.every(isAddress) &&
        typeof v.hi === 'number' &&
        v.hi >= -1 &&
        v.hi < v.hist.length
      );
  }
  return false;
};

const REQUEST_KINDS: ReadonlySet<string> = new Set([
  'show',
  'tools',
  'browser',
  'design',
  'files',
  'plan',
  'subagent',
  'terminal',
  'side',
  'legacy',
]);

const isRequest = (v: unknown): v is IPaneRequest =>
  isRecord(v) &&
  isString(v.k) &&
  REQUEST_KINDS.has(v.k) &&
  typeof v.reload === 'boolean' &&
  (v.target === undefined || stringOrNull(v.target)) &&
  (v.k !== 'legacy' || isString(v.kind));

const keptOf = (raw: unknown): IKeptPane | null => {
  if (!isRecord(raw) || !Array.isArray(raw.tabs) || typeof raw.at !== 'number') return null;
  const tabs = raw.tabs.filter(isPaneTab);
  const ids = new Set(tabs.map((tab) => tab.id));
  return {
    machine: isString(raw.machine) ? raw.machine : null,
    at: raw.at,
    open: raw.open === true,
    maxed: raw.maxed === true,
    width: typeof raw.width === 'number' && raw.width > 0 ? raw.width : null,
    tabs,
    // A Legacy tab ('L:…') is Legacy's to keep: it may come back, so the front stays on it.
    active: isString(raw.active) ? raw.active : '',
    unread: Array.isArray(raw.unread)
      ? raw.unread.filter((id): id is string => isString(id) && ids.has(id))
      : [],
    view: isRecord(raw.view) ? raw.view : {},
    pending: Array.isArray(raw.pending) ? raw.pending.filter(isRequest).slice(-MAX_PENDING) : [],
  };
};

/** The kept panes as they were written; an empty map when there are none or they are not ours. */
export const parseKept = (raw: string | null): TKept => {
  const kept = new Map<string, IKeptPane>();
  if (!raw) return kept;
  try {
    const data = JSON.parse(raw) as { v?: unknown; chats?: unknown };
    if (data.v !== VERSION || !isRecord(data.chats)) return kept;
    for (const [conv, entry] of Object.entries(data.chats)) {
      const pane = keptOf(entry);
      if (pane) kept.set(conv, pane);
    }
  } catch {
    return kept;
  }
  return capKept(kept);
};

/** The `max` most recent. */
export const capKept = (kept: TKept, max: number = MAX_CHATS): TKept => {
  if (kept.size <= max) return kept;
  return new Map([...kept].sort((a, b) => b[1].at - a[1].at).slice(0, max));
};

export const serializeKept = (kept: TKept): string =>
  JSON.stringify({ v: VERSION, chats: Object.fromEntries(capKept(kept)) });

export interface IPruneContext {
  /** The machine's own list has come: what it lacks is gone. */
  listed: (machine: string) => boolean;
  exists: (conv: string) => boolean;
}

/** Without the chats their machine no longer lists; the same map when nothing went. */
export const pruneKept = (kept: TKept, ctx: IPruneContext): TKept => {
  const gone = [...kept].filter(
    ([conv, pane]) => pane.machine !== null && ctx.listed(pane.machine) && !ctx.exists(conv),
  );
  if (gone.length === 0) return kept;
  const next = new Map(kept);
  for (const [conv] of gone) next.delete(conv);
  return next;
};

/** The chat's pane after `change`, now the most recent; the same map when nothing changed. */
export const updateKept = (
  kept: TKept,
  conv: string,
  machine: string | null,
  change: (pane: IChatPane) => IChatPane,
  now: number,
): TKept => {
  const before = kept.get(conv);
  const pane = change(before ?? EMPTY_PANE);
  if (pane === (before ?? EMPTY_PANE)) return kept;
  const next = new Map(kept);
  next.set(conv, { ...pane, machine: machine ?? before?.machine ?? null, at: now });
  return capKept(next);
};

/** Waits a request for the chat; the newest few are kept. */
export const queueRequest = (pane: IChatPane, request: IPaneRequest): IChatPane => ({
  ...pane,
  pending: [...pane.pending, request].slice(-MAX_PENDING),
});

// ── The store ──

const storage = {
  read(): string | null {
    try {
      return localStorage.getItem(PANE_STATE_KEY);
    } catch {
      return null;
    }
  },
  write(value: string) {
    try {
      localStorage.setItem(PANE_STATE_KEY, value);
    } catch {
      // Private mode or a full storage: the panes are kept for this page and forgotten on reload.
    }
  },
};

const listeners = new Set<() => void>();
let kept: TKept | null = null;
let persistTimer: ReturnType<typeof setTimeout> | null = null;
let hooked = false;

const persistNow = () => {
  if (persistTimer !== null) clearTimeout(persistTimer);
  persistTimer = null;
  if (kept) storage.write(serializeKept(kept));
};

const all = (): TKept => (kept ??= parseKept(storage.read()));

const commit = (next: TKept) => {
  if (next === kept) return;
  kept = next;
  if (!hooked && typeof window !== 'undefined') {
    hooked = true;
    // The page going away (a reload, a closed tab) writes at once what the timer has not yet.
    window.addEventListener('pagehide', persistNow);
  }
  persistTimer ??= setTimeout(persistNow, PERSIST_MS);
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** The chat's pane now: what was kept, or the empty one. */
export const chatPaneOf = (conv: string | null): IChatPane =>
  (conv === null ? undefined : all().get(conv)) ?? EMPTY_PANE;

export const chatPanes = {
  /** Changes the chat's pane, on screen or not. */
  update: (conv: string, machine: string | null, change: (pane: IChatPane) => IChatPane) =>
    commit(updateKept(all(), conv, machine, change, Date.now())),
  /** A request for a chat that is not on screen: it waits for the person there. */
  queue: (conv: string, machine: string | null, request: IPaneRequest) =>
    commit(updateKept(all(), conv, machine, (pane) => queueRequest(pane, request), Date.now())),
  /** Forgets the chats their machine no longer lists. */
  prune: (ctx: IPruneContext) => commit(pruneKept(all(), ctx)),
  /** Writes at once (tests, the page going away). */
  flush: persistNow,
};

/** Forgets the page's panes (tests). */
export const resetChatPanes = () => {
  if (persistTimer !== null) clearTimeout(persistTimer);
  persistTimer = null;
  kept = null;
};

export const useChatPane = (conv: string | null): IChatPane => {
  const get = () => chatPaneOf(conv);
  return useSyncExternalStore(subscribe, get, get);
};

/**
 * A piece of a pane view's state that belongs to the chat (the pages a design is open on): another chat has its own,
 * coming back or a reload brings it again. The value must be JSON. With no chat it is the view's own state.
 */
export const useChatPaneView = <T>(
  conv: string | null,
  key: string,
  initial: T,
): [T, (next: T | ((current: T) => T)) => void] => {
  const pane = useChatPane(conv);
  const [first] = useState(initial);
  const [own, setOwn] = useState<T>(first);
  const value = conv === null ? own : ((pane.view[key] as T | undefined) ?? first);
  const set = useCallback(
    (next: T | ((current: T) => T)) => {
      if (conv === null) {
        setOwn(next);
        return;
      }
      chatPanes.update(conv, null, (now) => {
        const current = (now.view[key] as T | undefined) ?? first;
        const value = typeof next === 'function' ? (next as (current: T) => T)(current) : next;
        return Object.is(value, current) ? now : { ...now, view: { ...now.view, [key]: value } };
      });
    },
    [conv, key, first],
  );
  return [value, set];
};
