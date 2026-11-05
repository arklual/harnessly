// A site of the pane's browser that redirects to another one: the slot never follows a redirect to another
// origin, it shows an offer page. A hop between our sites — the site, its sign-in (Keycloak, Teleport) and back — the
// tab follows by itself in place of the page, so a sign-in goes through in the pane; any other site waits for the
// person on the offer page. A loop of redirects stops after a few hops.
//
// A Teleport application is a chain of its own: the app `<name>.<proxy>` sends a person without a session to
// `<proxy>/web/launch/<name>.<proxy>`, the proxy signs them in (its single sign-on, a sign-in host of ours) and sends
// them back to `<name>.<proxy>/x-teleport-auth`. When the proxy is ours (a site of the list, `tl.example.ru`, say),
// those two hops are followed whatever the app is; the proxy's hop to its sign-in and back is one between our sites.
import { siteCovers } from '@/next/features/chat/pane-sites';

/** Hops followed in a row before the tab stops and leaves the offer page to the person. */
export const MAX_HOPS = 8;
/** Hops this far apart are not a loop. */
export const HOP_WINDOW_MS = 30_000;

const ours = (u: URL, sites: readonly string[], signInHosts: readonly string[]): boolean =>
  signInHosts.some((h) => h.toLowerCase() === u.host.toLowerCase()) ||
  sites.some((site) => siteCovers(site, u));

const httpsUrl = (raw: string | null): URL | null => {
  if (raw === null) return null;
  try {
    const u = new URL(raw);
    return u.protocol === 'https:' && !u.username && !u.password ? u : null;
  } catch {
    return null;
  }
};

/** `app` is `<one label>.<proxy>`, both on the default port. */
const appOf = (app: URL, proxy: URL): boolean => {
  if (app.port || proxy.port) return false;
  const dot = app.hostname.indexOf('.');
  return dot > 0 && app.hostname.slice(dot + 1) === proxy.hostname;
};

/** A hop of a Teleport application's sign-in: the app to its proxy's launcher of this app, the proxy back to the app's
 *  exchange of the session. */
export const teleportHop = (to: URL, at: URL): boolean => {
  if (appOf(at, to)) {
    const launch = `/web/launch/${at.hostname}`;
    return to.pathname === launch || to.pathname.startsWith(`${launch}/`);
  }
  return appOf(to, at) && to.pathname === '/x-teleport-auth';
};

/**
 * Whether the tab follows a redirect by itself: from one of our https sites to another — a site of the pane's list,
 * named outright, or a sign-in host of ours — or a hop of a Teleport application's sign-in through a proxy of ours. A
 * server of the machine or any other site waits for the person.
 */
export const followsRedirect = (
  url: string,
  from: string | null,
  sites: readonly string[],
  signInHosts: readonly string[],
): boolean => {
  const to = httpsUrl(url);
  const at = httpsUrl(from);
  if (!to || !at) return false;
  if (teleportHop(to, at)) return ours(appOf(at, to) ? to : at, sites, signInHosts);
  return ours(to, sites, signInHosts) && ours(at, sites, signInHosts);
};

/** Counts the hops of a tab: true while it may take one more now. */
export const makeHops = (max = MAX_HOPS, windowMs = HOP_WINDOW_MS) => {
  let times: number[] = [];
  return (now: number): boolean => {
    times = times.filter((at) => now - at < windowMs);
    if (times.length >= max) return false;
    times.push(now);
    return true;
  };
};
