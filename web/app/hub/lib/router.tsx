// A small router over the History API: the hub UI has a dozen routes and no need for a library. Paths are matched in
// order; `:name` takes one segment. Links are real anchors (middle-click and "open in a new tab" work); a plain click
// is handled here.
import { type AnchorHTMLAttributes, type MouseEvent, useEffect, useMemo, useSyncExternalStore } from 'react';

export interface Match {
  route: string;
  params: Record<string, string>;
}

/** Matches `path` against route patterns like `/s/:slug/setup/machines`; the first that fits wins. */
export const matchRoute = (patterns: readonly string[], path: string): Match | null => {
  const parts = path.split('/').filter(Boolean);
  for (const route of patterns) {
    const want = route.split('/').filter(Boolean);
    if (want.length !== parts.length) continue;
    const params: Record<string, string> = {};
    const ok = want.every((seg, i) => {
      const got = parts[i] ?? '';
      if (seg.startsWith(':')) {
        try {
          params[seg.slice(1)] = decodeURIComponent(got);
        } catch {
          return false;
        }
        return true;
      }
      return seg === got;
    });
    if (ok) return { route, params };
  }
  return null;
};

/** A path the UI may send the browser to after sign-in: same origin, no scheme, no `//`. */
export const safeNext = (next: string | null | undefined): string | null => {
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.startsWith('/\\')) return null;
  if (next.startsWith('/api/')) return null;
  return next;
};

/** A path the hub answers itself (the machines' pages under /m/): not a screen of this UI, so it takes a full load. */
export const isOutside = (path: string): boolean => path === '/m' || path.startsWith('/m/');

export interface Loc {
  path: string;
  search: string;
}

const listeners = new Set<() => void>();
let snapshot: Loc | null = null;

const read = (): Loc => {
  const path = location.pathname;
  const search = location.search;
  if (!snapshot || snapshot.path !== path || snapshot.search !== search) snapshot = { path, search };
  return snapshot;
};

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  window.addEventListener('popstate', fn);
  return () => {
    listeners.delete(fn);
    window.removeEventListener('popstate', fn);
  };
};

/** Goes to a path of the hub UI. */
export const navigate = (to: string, opts: { replace?: boolean } = {}) => {
  if (isOutside(to)) {
    if (opts.replace) location.replace(to);
    else location.assign(to);
    return;
  }
  if (opts.replace) history.replaceState(null, '', to);
  else history.pushState(null, '', to);
  listeners.forEach((fn) => fn());
  window.scrollTo(0, 0);
};

export const useLocation = (): Loc => useSyncExternalStore(subscribe, read, read);

export const useQuery = (): URLSearchParams => {
  const { search } = useLocation();
  return useMemo(() => new URLSearchParams(search), [search]);
};

interface LinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> {
  to: string;
  replace?: boolean;
}

export const Link = ({ to, replace, onClick, children, ...rest }: LinkProps) => {
  const onLink = (e: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(e);
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (rest.target && rest.target !== '_self') return;
    e.preventDefault();
    if (replace) navigate(to, { replace: true });
    else navigate(to);
  };
  return (
    <a {...rest} href={to} onClick={onLink}>
      {children}
    </a>
  );
};

/** Redirects in an effect (never during render). */
export const Redirect = ({ to }: { to: string }) => {
  useEffect(() => {
    navigate(to, { replace: true });
  }, [to]);
  return null;
};
