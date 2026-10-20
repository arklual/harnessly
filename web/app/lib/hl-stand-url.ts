/**
 * A feature stand — `https://<slug><standDomain>`, made by a pipeline an agent started — is the one external
 * address the right panel opens without the person typing it: from a command of the agent or by itself when the
 * conversation names it. The list is this one shape and nothing else, so an address that came in through a tool's
 * output cannot put a page of its choosing in the trusted panel. The panel's own slots (`ai-…`) and the sign-in host
 * (`auth.`) of the stands are not stands. No stand domain configured: nothing is a stand.
 */
import { appConfig } from '@/platform/config';

const LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;
const NOT_STANDS = /^(?:ai-|auth$)/;

export const isStandHost = (host: string): boolean => {
  const domain = appConfig().standDomain;
  if (!domain || typeof host !== 'string' || !host.endsWith(domain)) return false;
  const label = host.slice(0, -domain.length);
  return LABEL.test(label) && !NOT_STANDS.test(label);
};

/** `https://<stand>` exactly as the URL parser prints an origin: no port, no other spelling of the host. */
export const isStandOrigin = (origin: string): boolean => {
  let url: URL;
  try {
    url = new URL(origin);
  } catch {
    return false;
  }
  return url.protocol === 'https:' && !url.port && url.origin === origin && isStandHost(url.hostname);
};

/** The stand's address as the browser tab takes it (`https://host/path`), or null for anything that is not a stand. */
export const standUrlOf = (text: string): string | null => {
  let url: URL;
  try {
    url = new URL(text.trim());
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || !isStandHost(url.hostname))
    return null;
  const path = `${url.pathname}${url.search}`;
  return `https://${url.hostname}${path === '/' ? '' : path}`;
};

const escape = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

/** Stand addresses written in a text, in order (a pattern that matches nothing without a stand domain). */
export const standUrlPattern = (): RegExp => {
  const domain = appConfig().standDomain;
  return domain
    ? new RegExp(`\\bhttps:\\/\\/[a-z0-9-]+${escape(domain)}(?:\\/[^\\s"'\`<>()[\\]{}]*)?`, 'gi')
    : /(?!)/g;
};

const OAUTH2_PATH = '/oauth2/';
/** Where a stand sends a person without a session: the identity provider and the stands' auth proxy. */
const signInHosts = (): ReadonlySet<string> => new Set(appConfig().standSignInHosts);

/** Is `next`, where the stand at `standOrigin` redirected, the way to its sign-in rather than a page of the stand? */
export const isStandSignInRedirect = (standOrigin: string, next: URL): boolean => {
  if (next.protocol !== 'https:' || next.username || next.password) return false;
  if (next.origin === standOrigin) return next.pathname.startsWith(OAUTH2_PATH);
  return !next.port && signInHosts().has(next.hostname);
};

/**
 * The address that signs a person in to the stand and brings them back to `pageUrl`, or null when `pageUrl` is not a
 * stand. It is oauth2-proxy's own start, first-party on the stand's host, so the session lands on the stand domain.
 */
export const standSignInUrl = (pageUrl: string): string | null => {
  if (!standUrlOf(pageUrl)) return null;
  const page = new URL(pageUrl);
  const start = new URL(`${OAUTH2_PATH}start`, page.origin);
  start.searchParams.set(
    'rd',
    page.pathname.startsWith(OAUTH2_PATH) ? page.origin : `${page.origin}${page.pathname}${page.search}`,
  );
  return start.href;
};
