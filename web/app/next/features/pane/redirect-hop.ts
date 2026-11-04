// A site of the pane's browser that redirects to another one: the slot never follows a redirect to another
// origin, it shows an offer page. A hop between our sites — the site, its sign-in (Keycloak, Teleport) and back — the
// tab follows by itself in place of the page, so a sign-in goes through in the pane; any other site waits for the
// person on the offer page. A loop of redirects stops after a few hops.
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

/**
 * Whether the tab follows a redirect by itself: from one of our https sites to another — a site of the pane's list,
 * named outright, or a sign-in host of ours. A server of the machine or any other site waits for the person.
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
