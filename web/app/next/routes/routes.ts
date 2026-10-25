// The new interface's addresses over @/platform/router: `/` is the greeting, `/c/:id` a chat (its id readable, the
// colons of a key kept as they are). The development galleries (`?dev=<name>`) are opened by main.tsx.
import Router, { useRouter } from '@/platform/router';

export const ROUTE = {
  home: 'home',
  chat: 'chat',
  designly: 'designly',
  site: 'site',
  invite: 'invite',
  stats: 'stats',
  efficiency: 'efficiency',
  apps: 'apps',
  settings: 'settings',
} as const;

/** The sections of the Settings screen, in the order of its list. */
export const SETTINGS_SECTIONS = [
  'general',
  'notifications',
  'accounts',
  'models',
  'claude',
  'codex',
  'skills',
  'machines',
  'apps',
  'beta',
] as const;
export type SettingsSection = (typeof SETTINGS_SECTIONS)[number];

/** `?access=` of a design: the share dialog opens on its people or on its requests (a push says which). */
export const ACCESS_FOCUS = ['people', 'requests'] as const;
export type TAccessFocus = (typeof ACCESS_FOCUS)[number];

export type TRoute =
  | { kind: typeof ROUTE.home }
  | { kind: typeof ROUTE.chat; id: string }
  | { kind: typeof ROUTE.designly }
  /** A design of the library opened: `/designly/:id`, `?chat=` the chat it was opened from (its column shows it). */
  | { kind: typeof ROUTE.site; id: string; chat?: string; access?: TAccessFocus }
  /** A link-invitation to a design, `/designly/invite/:token` (a push or a letter says `/design/invite/:token`). */
  | { kind: typeof ROUTE.invite; token: string }
  /** The screens of «More»: `/stats`, `/efficiency`, `/apps`. */
  | { kind: typeof ROUTE.stats }
  | { kind: typeof ROUTE.efficiency }
  | { kind: typeof ROUTE.apps }
  /** Settings: `/settings` (the list of sections; on a wide window the first one is open) and `/settings/:section`. */
  | { kind: typeof ROUTE.settings; section: SettingsSection | null };

export const HOME: TRoute = { kind: ROUTE.home };

const CHAT_RE = /^\/c\/([^/]+)\/?$/;
const DESIGNLY_RE = /^\/designly\/?$/;
const SITE_RE = /^\/design(?:ly)?\/([a-z0-9][a-z0-9-]{0,62})\/?$/;
const INVITE_RE = /^\/design(?:ly)?\/invite\/([A-Za-z0-9_-]{8,200})\/?$/;
const STATS_RE = /^\/stats\/?$/;
const EFFICIENCY_RE = /^\/efficiency\/?$/;
const APPS_RE = /^\/apps\/?$/;
const SETTINGS_RE = /^\/settings(?:\/([a-z]+))?\/?$/;

/** One parameter of an address's query, when it names one. */
const queryParam = (path: string, name: string): string | null => {
  const query = path.split('#')[0]?.split('?')[1];
  if (!query) return null;
  try {
    return new URLSearchParams(query).get(name) || null;
  } catch {
    return null;
  }
};

/** The route of an address inside the app (`/c/tab-1`, `/`); anything unknown is the greeting. */
export const parseRoute = (path: string): TRoute => {
  const pathname = path.split(/[?#]/)[0] ?? '/';
  if (DESIGNLY_RE.test(pathname)) return { kind: ROUTE.designly };
  if (STATS_RE.test(pathname)) return { kind: ROUTE.stats };
  if (EFFICIENCY_RE.test(pathname)) return { kind: ROUTE.efficiency };
  if (APPS_RE.test(pathname)) return { kind: ROUTE.apps };
  const settings = SETTINGS_RE.exec(pathname);
  if (settings) {
    const section = SETTINGS_SECTIONS.find((id) => id === settings[1]);
    if (!settings[1]) return { kind: ROUTE.settings, section: null };
    return section ? { kind: ROUTE.settings, section } : HOME;
  }
  const invite = INVITE_RE.exec(pathname);
  if (invite?.[1]) return { kind: ROUTE.invite, token: invite[1] };
  const site = SITE_RE.exec(pathname);
  if (site?.[1]) {
    const chat = queryParam(path, 'chat');
    const focus = ACCESS_FOCUS.find((x) => x === queryParam(path, 'access'));
    return {
      kind: ROUTE.site,
      id: site[1],
      ...(chat ? { chat } : {}),
      ...(focus ? { access: focus } : {}),
    };
  }
  const match = CHAT_RE.exec(pathname);
  if (!match?.[1]) return HOME;
  try {
    return { kind: ROUTE.chat, id: decodeURIComponent(match[1]) };
  } catch {
    return HOME;
  }
};

/** The address of a route inside the app, without the base path. */
export const routePath = (route: TRoute): string => {
  switch (route.kind) {
    case ROUTE.chat:
      return `/c/${encodeURIComponent(route.id).replace(/%3A/gi, ':')}`;
    case ROUTE.designly:
      return '/designly';
    case ROUTE.site: {
      const query = new URLSearchParams();
      if (route.chat) query.set('chat', route.chat);
      if (route.access) query.set('access', route.access);
      return query.size > 0 ? `/designly/${route.id}?${query}` : `/designly/${route.id}`;
    }
    case ROUTE.invite:
      return `/designly/invite/${route.token}`;
    case ROUTE.stats:
      return '/stats';
    case ROUTE.efficiency:
      return '/efficiency';
    case ROUTE.apps:
      return '/apps';
    case ROUTE.settings:
      return route.section ? `/settings/${route.section}` : '/settings';
    default:
      return '/';
  }
};

/** An address that is no route of ours, as one key: no query, no hash, no trailing slash, escapes read. */
const plainPathname = (path: string): string => {
  const raw = path.split(/[?#]/)[0] || '/';
  let decoded = raw;
  try {
    decoded = decodeURIComponent(raw);
  } catch {
    // A broken escape stays as it was written.
  }
  return decoded.replace(/\/+$/, '') || '/';
};

/**
 * The place an address is, as one key: two addresses with the same key show the same thing, so a tab
 * strip keeps one tab for them. A chat is `chat:<id>`; a design `design:<id>`, with `+chat:<id>` when it was opened
 * with a chat; Designly's library `designly`; every section of Settings `settings`; the start page `home`. Query
 * (page, board, panes, view), hash, a trailing slash, escapes and the `/design/` spelling do not make a place; an
 * address that is none of ours is its plain pathname.
 */
export const placeKeyOf = (path: string): string => {
  const route = parseRoute(path);
  switch (route.kind) {
    case ROUTE.chat:
      return `chat:${route.id}`;
    case ROUTE.site:
      return route.chat ? `design:${route.id}+chat:${route.chat}` : `design:${route.id}`;
    case ROUTE.invite:
      return `invite:${route.token}`;
    case ROUTE.home: {
      const pathname = plainPathname(path);
      return pathname === '/' ? 'home' : pathname;
    }
    default:
      return route.kind;
  }
};

export const sameRoute = (a: TRoute, b: TRoute) => routePath(a) === routePath(b);

/** Moves the screen: a push per chat (Back and Forward go between chats), replace on request. */
export const goTo = (next: TRoute, options: { replace?: boolean } = {}) => {
  if (sameRoute(next, parseRoute(Router.asPath))) return;
  const href = routePath(next);
  void (options.replace ? Router.replace(href) : Router.push(href));
};

/** The route on screen; it follows Back, Forward and links. */
export const useRoute = (): TRoute => parseRoute(useRouter().asPath);
