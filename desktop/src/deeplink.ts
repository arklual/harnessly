export const DEEP_LINK_PROTOCOL = 'harnessly';

/**
 * harnessly://open/<path>?<query> becomes an in-app path on the origin the window already shows.
 * The link never names a host, so a crafted link cannot send the window to another site.
 */
export const parseDeepLink = (raw: string): string | null => {
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== `${DEEP_LINK_PROTOCOL}:`) return null;
  if (url.hostname !== 'open') return null;
  const path = decodeURIComponent(url.pathname || '/');
  if (path.includes('..') || path.includes('\\') || path.startsWith('//')) return null;
  return (path.startsWith('/') ? path : `/${path}`) + url.search + url.hash;
};

export const findDeepLink = (argv: readonly string[]): string | null =>
  argv.find((arg) => arg.startsWith(`${DEEP_LINK_PROTOCOL}://`)) ?? null;

export const applyDeepLink = (origin: string, raw: string): string | null => {
  const path = parseDeepLink(raw);
  return path === null ? null : origin.replace(/\/$/, '') + path;
};
