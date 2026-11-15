// The new interface's addresses over @/platform/router: `/` is the greeting, `/c/:id` a chat (its id readable, the
// colons of a key kept as they are). The development galleries (`?dev=<name>`) are opened by main.tsx.
import { useMemo } from 'react';
import Router, { useRouter } from '@/platform/router';
import { APP_OBJECT_RE } from '@/next/routes/app-object';

export const ROUTE = {
  home: 'home',
  chat: 'chat',
  designly: 'designly',
  designlyNew: 'designlyNew',
  videoNew: 'videoNew',
  site: 'site',
  kit: 'kit',
  appDoc: 'appDoc',
  invite: 'invite',
  manage: 'manage',
  stats: 'stats',
  efficiency: 'efficiency',
  tasks: 'tasks',
  apps: 'apps',
  app: 'app',
  appAbout: 'appAbout',
  review: 'review',
  help: 'help',
  learn: 'learn',
  settings: 'settings',
  chatgpt: 'chatgpt',
} as const;

/** The sections of the Settings screen, in the order of its list. */
export const SETTINGS_SECTIONS = [
  'general',
  'appearance',
  'keys',
  'notifications',
  'voice',
  'takes',
  'claude',
  'codex',
  'accounts',
  'smart',
  'skills',
  'safe',
  'browser',
  'machines',
  'apps',
  'connections',
  'design',
  'extensions',
  'plugins',
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
  /** The create screen of a presentation: `/designly/new/slides` (the Presentation tile of Designly's start). */
  | { kind: typeof ROUTE.designlyNew }
  /** The create screen of a video: `/designly/new/video` (the Video tile of Designly's start). */
  | { kind: typeof ROUTE.videoNew }
  /** A design of the library opened: `/designly/:id`, `?chat=` the chat it was opened from (its column shows it),
      `?m=` the machine it lives on (a reload opens it there, not on whichever machine is active),
      `?edit=1` the editing mode (without it the design is looked at), `?page=` the page on screen (its board's path in
      the project, HRN-1378). */
  | {
      kind: typeof ROUTE.site;
      id: string;
      page?: string;
      chat?: string;
      machine?: string;
      access?: TAccessFocus;
      edit?: boolean;
    }
  /** A kit's screen: `/designly/kit/:id`, `?v=` the version (the newest without it), `?theme=` the theme,
      `?node=` the node the camera starts on, `?chat=` the chat it was opened from («Отправить в чат» goes there). */
  | { kind: typeof ROUTE.kit; id: string; version?: string; theme?: string; node?: string; chat?: string }
  /** A document of a type an installed app adds, opened in the app's editor: `/designly/doc/:id`, `?chat=` the chat
      it was opened from (its mode is that chat's), `?edit=1` the editing mode. */
  | { kind: typeof ROUTE.appDoc; id: string; chat?: string; edit?: boolean }
  /** A link-invitation to a design, `/designly/invite/:token` (a push or a letter says `/design/invite/:token`). */
  | { kind: typeof ROUTE.invite; token: string }
  /** The screens of «More»: `/manage` (personal history, with Statistics and Efficiency as its tabs), `/apps`. */
  | { kind: typeof ROUTE.manage }
  | { kind: typeof ROUTE.stats }
  | { kind: typeof ROUTE.efficiency }
  | { kind: typeof ROUTE.tasks }
  /** The installed apps: `/apps`. */
  | { kind: typeof ROUTE.apps }
  /** An app opened as the main thing: `/apps/:id`, `?chat=` the chat it works in (its column shows it), `?o=` one of
   *  the app's objects open (a document, a board) instead of the app's home. */
  | { kind: typeof ROUTE.app; id: string; chat?: string; object?: string; thread?: string }
  /** The page about an app, installed or not: `/apps/:id/about`. */
  | { kind: typeof ROUTE.appAbout; id: string }
  /**
   * The built-in «Ревью кода»: `/review` its start (the merge requests), `?url=` a merge request or commit opened,
   * `?chat=` the chat beside it — alone, the work of that chat is what is reviewed; `?wt=` the worktree of that work.
   */
  | { kind: typeof ROUTE.review; url?: string; chat?: string; wt?: string }
  /**
   * The documentation: `/help` (its first page), `/help/:section` (the section's first page) and `/help/:section/:page`,
   * `#anchor` a heading of it. An older `/help/:page` of the apps guide comes as a section the page resolves.
   */
  | { kind: typeof ROUTE.help; section: string | null; page: string | null; anchor?: string }
  /** Settings: `/settings` (the list of sections; on a wide window the first one is open) and `/settings/:section`. */
  /** «Обучение»: the lessons, each a film and a hands-on tour. */
  | { kind: typeof ROUTE.learn }
  /** `connect`: Connections opens with this address already found, to sign in to it (`?connect=`). */
  | { kind: typeof ROUTE.settings; section: SettingsSection | null; connect?: string }
  /** A conversation with ChatGPT (internal/chatgpt): `/chatgpt/:id`, `?m=` the machine it is kept on. */
  | { kind: typeof ROUTE.chatgpt; id: string; machine?: string };

export const HOME: TRoute = { kind: ROUTE.home };

const CHAT_RE = /^\/c\/([^/]+)\/?$/;
const CHATGPT_RE = /^\/chatgpt\/([a-f0-9]{18})\/?$/;
const DESIGNLY_RE = /^\/designly\/?$/;
const DESIGNLY_NEW_RE = /^\/designly\/new\/slides\/?$/;
const VIDEO_NEW_RE = /^\/designly\/new\/video\/?$/;
const SITE_RE = /^\/design(?:ly)?\/([a-z0-9][a-z0-9-]{0,62})\/?$/;
const APP_DOC_RE = /^\/designly\/doc\/([a-z0-9][a-z0-9-]{0,62})\/?$/;
const KIT_RE = /^\/designly\/kit\/([a-z0-9][a-z0-9-]{0,62})\/?$/;
const INVITE_RE = /^\/design(?:ly)?\/invite\/([A-Za-z0-9_-]{8,200})\/?$/;
const MANAGE_RE = /^\/manage\/?$/;
const STATS_RE = /^\/stats\/?$/;
const EFFICIENCY_RE = /^\/efficiency\/?$/;
const TASKS_RE = /^\/tasks\/?$/;
const APPS_RE = /^\/apps\/?$/;
const REVIEW_RE = /^\/review\/?$/;
const APP_ABOUT_RE = /^\/apps\/([a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39})\/about\/?$/;
export { APP_OBJECT_RE };
const APP_RE = /^\/apps\/([a-z0-9][a-z0-9-]{0,39}\.[a-z0-9][a-z0-9-]{0,39})\/?$/;
const HELP_RE = /^\/help(?:\/([a-z0-9][a-z0-9-]{0,62})(?:\/([a-z0-9][a-z0-9-]{0,62}))?)?\/?$/;
const LEARN_RE = /^\/learn\/?$/;
const SETTINGS_RE = /^\/settings(?:\/([a-z]+))?\/?$/;

/** One parameter of an address's query, when it names one. */
/** A path of this page (`/apps/…`), not an address of another site or a protocol-relative one. */
export const isInAppPath = (href: string): boolean => /^\/(?!\/)/.test(href) && !href.includes('\\');

const queryParam = (path: string, name: string): string | null => {
  const query = path.split('#')[0]?.split('?')[1];
  if (!query) return null;
  try {
    return new URLSearchParams(query).get(name) || null;
  } catch {
    return null;
  }
};

/** The `#hash` of an address, read; null when it has none. */
const hashOf = (path: string): string | null => {
  const raw = path.split('#')[1];
  if (!raw) return null;
  try {
    return decodeURIComponent(raw);
  } catch {
    return null;
  }
};

/** The route of an address inside the app (`/c/tab-1`, `/`); anything unknown is the greeting. */
export const parseRoute = (path: string): TRoute => {
  const pathname = path.split(/[?#]/)[0] ?? '/';
  if (DESIGNLY_RE.test(pathname)) return { kind: ROUTE.designly };
  if (DESIGNLY_NEW_RE.test(pathname)) return { kind: ROUTE.designlyNew };
  if (VIDEO_NEW_RE.test(pathname)) return { kind: ROUTE.videoNew };
  const kit = KIT_RE.exec(pathname);
  if (kit?.[1]) {
    const version = queryParam(path, 'v');
    const theme = queryParam(path, 'theme');
    const node = queryParam(path, 'node');
    const chat = queryParam(path, 'chat');
    return {
      kind: ROUTE.kit,
      id: kit[1],
      ...(version ? { version } : {}),
      ...(theme ? { theme } : {}),
      ...(node ? { node } : {}),
      ...(chat ? { chat } : {}),
    };
  }
  if (MANAGE_RE.test(pathname)) return { kind: ROUTE.manage };
  if (STATS_RE.test(pathname)) return { kind: ROUTE.stats };
  if (EFFICIENCY_RE.test(pathname)) return { kind: ROUTE.efficiency };
  if (TASKS_RE.test(pathname)) return { kind: ROUTE.tasks };
  if (APPS_RE.test(pathname)) return { kind: ROUTE.apps };
  if (REVIEW_RE.test(pathname)) {
    const url = queryParam(path, 'url');
    const chat = queryParam(path, 'chat');
    const wt = queryParam(path, 'wt');
    return {
      kind: ROUTE.review,
      ...(url ? { url } : {}),
      ...(chat ? { chat } : {}),
      ...(wt ? { wt } : {}),
    };
  }
  if (LEARN_RE.test(pathname)) return { kind: ROUTE.learn };
  const gpt = CHATGPT_RE.exec(pathname);
  if (gpt?.[1]) {
    const machine = queryParam(path, 'm');
    return { kind: ROUTE.chatgpt, id: gpt[1], ...(machine ? { machine } : {}) };
  }
  const about = APP_ABOUT_RE.exec(pathname);
  if (about?.[1]) return { kind: ROUTE.appAbout, id: about[1] };
  const app = APP_RE.exec(pathname);
  if (app?.[1]) {
    const chat = queryParam(path, 'chat');
    const object = queryParam(path, 'o');
    const thread = queryParam(path, 'th');
    const opens = object !== null && APP_OBJECT_RE.test(object);
    return {
      kind: ROUTE.app,
      id: app[1],
      ...(opens ? { object } : {}),
      // A comment thread on the object (`?th=`, a mention in the bell): only with an object.
      ...(opens && thread && APP_OBJECT_RE.test(thread) ? { thread } : {}),
      ...(chat ? { chat } : {}),
    };
  }
  const help = HELP_RE.exec(pathname);
  if (help) {
    const anchor = hashOf(path);
    return {
      kind: ROUTE.help,
      section: help[1] ?? null,
      page: help[2] ?? null,
      ...(anchor ? { anchor } : {}),
    };
  }
  const settings = SETTINGS_RE.exec(pathname);
  if (settings) {
    // «Models» was a section of its own: its address leads to the page it moved to.
    const section = SETTINGS_SECTIONS.find(
      (id) => id === (settings[1] === 'models' ? 'accounts' : settings[1]),
    );
    if (!settings[1]) return { kind: ROUTE.settings, section: null };
    const connect = section === 'connections' ? queryParam(path, 'connect') : null;
    return section ? { kind: ROUTE.settings, section, ...(connect ? { connect } : {}) } : HOME;
  }
  const appDoc = APP_DOC_RE.exec(pathname);
  if (appDoc?.[1]) {
    const chat = queryParam(path, 'chat');
    return {
      kind: ROUTE.appDoc,
      id: appDoc[1],
      ...(chat ? { chat } : {}),
      ...(queryParam(path, 'edit') === '1' ? { edit: true } : {}),
    };
  }
  const invite = INVITE_RE.exec(pathname);
  if (invite?.[1]) return { kind: ROUTE.invite, token: invite[1] };
  const site = SITE_RE.exec(pathname);
  if (site?.[1]) {
    const chat = queryParam(path, 'chat');
    const machine = queryParam(path, 'm');
    const focus = ACCESS_FOCUS.find((x) => x === queryParam(path, 'access'));
    const page = queryParam(path, 'page');
    return {
      kind: ROUTE.site,
      id: site[1],
      ...(page ? { page } : {}),
      ...(chat ? { chat } : {}),
      ...(machine ? { machine } : {}),
      ...(focus ? { access: focus } : {}),
      ...(queryParam(path, 'edit') === '1' ? { edit: true } : {}),
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
    case ROUTE.designlyNew:
      return '/designly/new/slides';
    case ROUTE.videoNew:
      return '/designly/new/video';
    case ROUTE.site: {
      const query = new URLSearchParams();
      if (route.chat) query.set('chat', route.chat);
      if (route.machine) query.set('m', route.machine);
      if (route.access) query.set('access', route.access);
      if (route.edit) query.set('edit', '1');
      if (route.page) query.set('page', route.page);
      return query.size > 0 ? `/designly/${route.id}?${query}` : `/designly/${route.id}`;
    }
    case ROUTE.kit: {
      const query = new URLSearchParams();
      if (route.version) query.set('v', route.version);
      if (route.theme) query.set('theme', route.theme);
      if (route.node) query.set('node', route.node);
      if (route.chat) query.set('chat', route.chat);
      return query.size > 0 ? `/designly/kit/${route.id}?${query}` : `/designly/kit/${route.id}`;
    }
    case ROUTE.appDoc: {
      const query = new URLSearchParams();
      if (route.chat) query.set('chat', route.chat);
      if (route.edit) query.set('edit', '1');
      return query.size > 0 ? `/designly/doc/${route.id}?${query}` : `/designly/doc/${route.id}`;
    }
    case ROUTE.invite:
      return `/designly/invite/${route.token}`;
    case ROUTE.manage:
      return '/manage';
    case ROUTE.stats:
      return '/stats';
    case ROUTE.efficiency:
      return '/efficiency';
    case ROUTE.tasks:
      return '/tasks';
    case ROUTE.apps:
      return '/apps';
    case ROUTE.appAbout:
      return `/apps/${route.id}/about`;
    case ROUTE.review: {
      const query = new URLSearchParams();
      if (route.url) query.set('url', route.url);
      if (route.chat) query.set('chat', route.chat);
      if (route.wt) query.set('wt', route.wt);
      return query.size > 0 ? `/review?${query}`.replace(/%3A/gi, ':') : '/review';
    }
    case ROUTE.app: {
      const query = new URLSearchParams({
        ...(route.object ? { o: route.object } : {}),
        ...(route.object && route.thread ? { th: route.thread } : {}),
        ...(route.chat ? { chat: route.chat } : {}),
      }).toString();
      return query ? `/apps/${route.id}?${query}`.replace(/%3A/gi, ':') : `/apps/${route.id}`;
    }
    case ROUTE.help: {
      let base = '/help';
      if (route.section) base += `/${route.section}`;
      if (route.section && route.page) base += `/${route.page}`;
      return route.anchor ? `${base}#${encodeURIComponent(route.anchor)}` : base;
    }
    case ROUTE.learn:
      return '/learn';
    case ROUTE.chatgpt:
      return route.machine
        ? `/chatgpt/${route.id}?${new URLSearchParams({ m: route.machine })}`
        : `/chatgpt/${route.id}`;
    case ROUTE.settings:
      if (route.section && route.connect)
        return `/settings/${route.section}?${new URLSearchParams({ connect: route.connect })}`.replace(
          /%3A/gi,
          ':',
        );
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
 * with a chat; a design edited with its chat is that chat's place, `chat:<id>` (one tab: features/designly/chat-place); an app `app:<id>`, `app:<id>/<object>` with an object open; Designly's library `designly`; every section of Settings `settings`; the start page `home`. Query
 * (page, board, panes, view), hash, a trailing slash, escapes and the `/design/` spelling do not make a place; an
 * address that is none of ours is its plain pathname.
 */
export const placeKeyOf = (path: string): string => {
  const route = parseRoute(path);
  switch (route.kind) {
    case ROUTE.chat:
      return `chat:${route.id}`;
    case ROUTE.site:
      if (route.chat && route.edit) return `chat:${route.chat}`;
      return route.chat ? `design:${route.id}+chat:${route.chat}` : `design:${route.id}`;
    case ROUTE.appDoc:
      return `appdoc:${route.id}`;
    case ROUTE.invite:
      return `invite:${route.token}`;
    case ROUTE.app:
      return route.object ? `app:${route.id}/${route.object}` : `app:${route.id}`;
    case ROUTE.appAbout:
      return `about:${route.id}`;
    case ROUTE.chatgpt:
      return `chatgpt:${route.id}`;
    case ROUTE.review:
      return `review:${route.url ?? ''}+${route.chat ?? ''}`;
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

/** The route on screen; it follows Back, Forward and links. The same object while the address is the same:
 *  what is memoized on the route keeps. */
export const useRoute = (): TRoute => {
  const path = useRouter().asPath;
  return useMemo(() => parseRoute(path), [path]);
};
