// How the person left each chat's window, kept for the chat on the machine (the record's `view`, command
// `conversation.view`) so another device opens the chat the same way: the full screen it was on
// (stores/chat-screen.ts), where the chat stood by that screen (features/designly/site-layout.ts), the right pane and
// its tab (stores/pane-state.ts) and the place in the feed (features/chat/feed-place.ts). Those stores stay the cache
// that opens a chat at once; the machine's copy is the truth: a newer one is put into them, and what changes here is
// sent after a pause (bindings/use-chat-view-sync.ts). The last write wins (`at`).
import { isPlace, placeOf, rememberPlace, AT_BOTTOM, type TFeedPlace } from '@/next/features/chat/feed-place';
import { keepLayout, keptLayout, parseLayout, type ISiteLayout } from '@/next/features/designly/site-layout';
import { isPaneTab, chatPaneOf, chatPanes } from '@/next/stores/pane-state';
import type { TTab } from '@/next/features/pane/pane-tabs';
import { forgetScreen, keepScreen, keptScreen, screenOf } from '@/next/stores/chat-screen';
import { parseRoute, routePath, type TRoute } from '@/next/routes/routes';

export interface IViewPane {
  open: boolean;
  maxed: boolean;
  width: number | null;
  tabs: TTab[];
  active: string;
}

export interface IChatView {
  /** When the person last changed it, by the clock of the device that wrote it. */
  at: number;
  /** The address of the full screen the chat was left on; null: as a chat. */
  screen: string | null;
  /** The chat's place by each screen it has, by the layout's key. */
  layouts: Record<string, ISiteLayout>;
  pane: IViewPane;
  feed: TFeedPlace;
}

const KEYS = 'hx-chat-layout-keys';
const SEEN = 'hx-chat-view-at';
/** More chats than this are not a working set; the least recent are forgotten. */
export const MAX_KEPT = 100;
/** The most layout keys one chat remembers (its screens). */
const MAX_LAYOUTS = 8;

const readMap = <T>(key: string): Map<string, T> => {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(key) ?? '{}');
    return typeof raw === 'object' && raw !== null
      ? new Map(Object.entries(raw) as [string, T][])
      : new Map();
  } catch {
    return new Map();
  }
};
const writeMap = <T>(key: string, map: Map<string, T>) => {
  while (map.size > MAX_KEPT) map.delete(map.keys().next().value as string);
  try {
    localStorage.setItem(key, JSON.stringify(Object.fromEntries(map)));
  } catch {
    // Blocked or full storage: the view holds while the page is open.
  }
};

/** A screen of this chat keeps its layout under `key`: the view sends it with the chat. */
export const noteLayoutKey = (conv: string, key: string): void => {
  const map = readMap<string[]>(KEYS);
  const have = map.get(conv) ?? [];
  if (have.at(-1) === key) return;
  map.delete(conv);
  map.set(conv, [...have.filter((k) => k !== key), key].slice(-MAX_LAYOUTS));
  writeMap(KEYS, map);
};

/** The version of a chat's view this device has: the `at` it wrote or read last; 0 when it has none. */
export const seenAt = (conv: string): number => readMap<number>(SEEN).get(conv) ?? 0;
export const noteSeenAt = (conv: string, at: number): void => {
  const map = readMap<number>(SEEN);
  map.delete(conv);
  map.set(conv, at);
  writeMap(SEEN, map);
};

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

/** A view as the machine kept it, piece by piece: what is not one is left out, so an older or hand-edited one still opens. */
export const parseView = (raw: unknown): IChatView | null => {
  if (!isRecord(raw) || typeof raw.at !== 'number' || !Number.isFinite(raw.at)) return null;
  const screen =
    typeof raw.screen === 'string' && screenOf(parseRoute(raw.screen)) !== null ? raw.screen : null;
  const layouts: Record<string, ISiteLayout> = {};
  if (isRecord(raw.layouts))
    for (const [key, value] of Object.entries(raw.layouts)) {
      const layout = parseLayout(value);
      if (layout) layouts[key] = layout;
    }
  const p = isRecord(raw.pane) ? raw.pane : {};
  const tabs = Array.isArray(p.tabs) ? p.tabs.filter(isPaneTab) : [];
  const pane: IViewPane = {
    open: p.open === true,
    maxed: p.maxed === true,
    width: typeof p.width === 'number' && Number.isFinite(p.width) ? p.width : null,
    tabs,
    active:
      typeof p.active === 'string' && (p.active === '' || tabs.some((t) => t.id === p.active))
        ? p.active
        : '',
  };
  return { at: raw.at, screen, layouts, pane, feed: isPlace(raw.feed) ? raw.feed : AT_BOTTOM };
};

/** The view of the chat as this device has it now (`at` left to the one who sends it). */
export const viewOf = (conv: string): Omit<IChatView, 'at'> => {
  const kept = keptScreen(conv);
  const layouts: Record<string, ISiteLayout> = {};
  for (const key of readMap<string[]>(KEYS).get(conv) ?? []) {
    const layout = keptLayout(key);
    if (layout) layouts[key] = layout;
  }
  const { open, maxed, width, tabs, active } = chatPaneOf(conv);
  return {
    screen: kept ? screenAddress(kept, conv) : null,
    layouts,
    pane: { open, maxed, width, tabs: [...tabs], active },
    feed: placeOf(conv),
  };
};

// The kept screen's address as a string: the screens store keeps routes by path.
const screenAddress = (route: TRoute, conv: string): string | null => {
  const screen = screenOf(route);
  return screen && screen.conv === conv ? routePath(route) : null;
};

/** What changes the view, not when: two views that differ only by `at` are the same. */
export const viewSignature = (view: Omit<IChatView, 'at'>): string => JSON.stringify(view);

/** Nothing worth keeping: a chat the person never arranged. */
export const isPlain = (view: Omit<IChatView, 'at'>): boolean =>
  view.screen === null &&
  Object.keys(view.layouts).length === 0 &&
  !view.pane.open &&
  !view.pane.maxed &&
  view.pane.tabs.length === 0 &&
  view.feed.k === 'bottom';

const listeners = new Set<() => void>();
/** Told when a view from the machine was put into the stores (the chat opened at its old screen can go there). */
export const onViewApplied = (listener: () => void): (() => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** Puts a view the machine kept into this device's stores, as the person had left the chat. */
export const applyView = (
  conv: string,
  machine: string | null,
  view: IChatView,
  { keepPane = false } = {},
): void => {
  for (const [key, layout] of Object.entries(view.layouts)) {
    keepLayout(key, layout);
    noteLayoutKey(conv, key);
  }
  if (!keepPane) {
    chatPanes.update(conv, machine, (pane) => ({ ...pane, ...view.pane, unread: [] }), 'machine');
  }
  rememberPlace(conv, view.feed);
  const screen = view.screen === null ? null : parseRoute(view.screen);
  if (screen) keepScreen(conv, view.screen as string);
  else forgetScreen(conv);
  noteSeenAt(conv, view.at);
  for (const listener of listeners) listener();
};
