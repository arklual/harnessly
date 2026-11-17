// The full screen each chat was on, as the person left it: a design, a kit, a document, an app or a merge request opened
// on the whole window from that chat (the address carries `?chat=`). Coming back to the chat — from the sidebar, after a
// reload, a day later, from another tab — opens that screen again; leaving it for the chat by Back (or its «назад»)
// puts the chat as it is. The rest of a chat's view is kept by its own stores: the right pane and its tab
// (stores/pane-state.ts), the chat's place and size over a design (features/designly/site-layout.ts), the review's
// (bindings/review-app.tsx). This browser's only: localStorage, the most recent chats.
import { parseRoute, ROUTE, routePath, type TRoute } from '@/next/routes/routes';

const KEY = 'hx-chat-screens';
/** More chats than this are not a working set; the least recent are forgotten. */
export const MAX_SCREENS = 100;

/** The chat a full screen was opened from and the screen's address; null for any other route. */
export const screenOf = (route: TRoute): { conv: string; path: string } | null => {
  switch (route.kind) {
    case ROUTE.site:
    case ROUTE.kit:
    case ROUTE.appDoc:
    case ROUTE.app:
    case ROUTE.review:
      return route.chat ? { conv: route.chat, path: routePath(route) } : null;
    default:
      return null;
  }
};

/** The kept screens, most recent last. */
export const parseScreens = (raw: string | null): Map<string, string> => {
  const out = new Map<string, string>();
  if (raw === null) return out;
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return out;
    for (const [conv, path] of Object.entries(value)) {
      // Only an address that is still a screen of this chat is kept (an older shape, a hand edit).
      if (typeof path === 'string' && screenOf(parseRoute(path))?.conv === conv) out.set(conv, path);
    }
  } catch {
    // A broken record: nothing was kept.
  }
  return out;
};

const read = (): Map<string, string> => {
  try {
    return parseScreens(localStorage.getItem(KEY));
  } catch {
    return new Map();
  }
};

const write = (screens: ReadonlyMap<string, string>) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(Object.fromEntries(screens)));
  } catch {
    // Blocked or full storage: the screen holds while the page is open.
  }
};

/** The screen the chat was left on, or null when it was left as a chat. */
export const keptScreen = (conv: string): TRoute | null => {
  const path = read().get(conv);
  return path === undefined ? null : parseRoute(path);
};

export const keepScreen = (conv: string, path: string) => {
  const screens = read();
  if (screens.get(conv) === path) return;
  screens.delete(conv);
  screens.set(conv, path);
  while (screens.size > MAX_SCREENS) screens.delete(screens.keys().next().value as string);
  write(screens);
};

export const forgetScreen = (conv: string) => {
  const screens = read();
  if (screens.delete(conv)) write(screens);
};

/** Chats asked for as a chat: a card of Главная opens the item's chat, the item is all there (the app's screen is not). */
const askedAsChat = new Set<string>();

/** The chat opens as a chat, not on the screen it was left on (another device's too): that screen is forgotten. */
export const openAsChat = (conv: string) => {
  forgetScreen(conv);
  askedAsChat.add(conv);
};

/** True once for a chat asked for as a chat: it is opening now. */
export const takeAsChat = (conv: string): boolean => askedAsChat.delete(conv);
