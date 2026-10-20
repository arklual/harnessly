// A small client router with the surface of next/router that the pages use: query, pathname, push, replace,
// isReady and route events. Addresses live under the app base path the runtime serves the page from.
import { useSyncExternalStore } from 'react';

import { appBasePath } from './config';

type TQuery = Record<string, string | string[] | undefined>;
type TListener = (url: string) => void;
type TEvent =
  | 'routeChangeStart'
  | 'routeChangeComplete'
  | 'beforeHistoryChange'
  | 'hashChangeStart'
  | 'hashChangeComplete'
  | 'routeChangeError';

const listeners = new Map<TEvent, Set<TListener>>();
const subscribers = new Set<() => void>();

const emit = (event: TEvent, url: string) => {
  for (const fn of listeners.get(event) ?? []) fn(url);
};

/** The path inside the app: `/agents/docs/x` with base `/agents` is `/docs/x`. */
const innerPath = (pathname: string): string => {
  const base = appBasePath();
  if (base && (pathname === base || pathname.startsWith(`${base}/`)))
    return pathname.slice(base.length) || '/';
  return pathname || '/';
};

/** Dynamic segments of the pages this app has: /docs/[slug]. */
const ROUTES: { pattern: RegExp; pathname: string; params: string[] }[] = [
  { pattern: /^\/docs\/([^/]+)\/?$/, pathname: '/docs/[slug]', params: ['slug'] },
];

interface ISnapshot {
  pathname: string;
  asPath: string;
  query: TQuery;
}

let snapshot: ISnapshot = read();

function read(): ISnapshot {
  if (typeof window === 'undefined') return { pathname: '/', asPath: '/', query: {} };
  const inner = innerPath(window.location.pathname);
  const query: TQuery = {};
  for (const [key, value] of new URLSearchParams(window.location.search)) {
    const had = query[key];
    query[key] = had === undefined ? value : Array.isArray(had) ? [...had, value] : [had, value];
  }
  let pathname = inner === '/' ? '/workspace' : inner;
  for (const route of ROUTES) {
    const match = route.pattern.exec(inner);
    if (!match) continue;
    pathname = route.pathname;
    route.params.forEach((name, i) => {
      query[name] = decodeURIComponent(match[i + 1] ?? '');
    });
  }
  return { pathname, asPath: `${inner}${window.location.search}${window.location.hash}`, query };
}

const refresh = () => {
  snapshot = read();
  for (const fn of subscribers) fn();
};

if (typeof window !== 'undefined') window.addEventListener('popstate', refresh);

type TUrl = string | { pathname?: string; query?: Record<string, unknown>; hash?: string };

const toHref = (url: TUrl): string => {
  if (typeof url === 'string') {
    if (/^[a-z]+:/i.test(url) || url.startsWith('?') || url.startsWith('#')) return url;
    return `${appBasePath()}${url.startsWith('/') ? url : `/${url}`}`;
  }
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(url.query ?? {})) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) value.forEach((v) => params.append(key, String(v)));
    else params.set(key, String(value));
  }
  const qs = params.toString();
  const path = url.pathname
    ? `${appBasePath()}${url.pathname === '/workspace' ? '/' : url.pathname}`
    : window.location.pathname;
  return `${path}${qs ? `?${qs}` : ''}${url.hash ? `#${url.hash}` : ''}`;
};

const navigate = (
  mode: 'push' | 'replace',
  url: TUrl,
  _as?: unknown,
  _options?: { shallow?: boolean; scroll?: boolean },
) => {
  const href = toHref(url);
  emit('routeChangeStart', href);
  emit('beforeHistoryChange', href);
  if (mode === 'push') window.history.pushState(window.history.state, '', href);
  else window.history.replaceState(window.history.state, '', href);
  refresh();
  emit('routeChangeComplete', href);
  return Promise.resolve(true);
};

export interface IRouter extends ISnapshot {
  isReady: boolean;
  route: string;
  basePath: string;
  push: (url: TUrl, as?: unknown, options?: { shallow?: boolean; scroll?: boolean }) => Promise<boolean>;
  replace: (url: TUrl, as?: unknown, options?: { shallow?: boolean; scroll?: boolean }) => Promise<boolean>;
  back: () => void;
  reload: () => void;
  prefetch: (url: string) => Promise<void>;
  events: {
    on: (event: TEvent, fn: TListener) => void;
    off: (event: TEvent, fn: TListener) => void;
    emit: (event: TEvent, url: string) => void;
  };
  beforePopState: (cb: (state: unknown) => boolean) => void;
}

const events: IRouter['events'] = {
  on: (event, fn) => {
    if (!listeners.has(event)) listeners.set(event, new Set());
    listeners.get(event)?.add(fn);
  },
  off: (event, fn) => listeners.get(event)?.delete(fn),
  emit,
};

const router = (): IRouter => ({
  ...snapshot,
  isReady: true,
  route: snapshot.pathname,
  basePath: appBasePath(),
  push: (url, as, options) => navigate('push', url, as, options),
  replace: (url, as, options) => navigate('replace', url, as, options),
  back: () => window.history.back(),
  reload: () => window.location.reload(),
  prefetch: () => Promise.resolve(),
  events,
  beforePopState: () => {},
});

const Router: IRouter = new Proxy({} as IRouter, { get: (_target, key) => router()[key as keyof IRouter] });

export const useRouter = (): IRouter => {
  useSyncExternalStore(
    (fn) => {
      subscribers.add(fn);
      return () => subscribers.delete(fn);
    },
    () => snapshot,
    () => snapshot,
  );
  return router();
};

export default Router;
