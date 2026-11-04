// The one spelling of a site a password belongs to (a copy of lib/vault/vault-origin.ts): `https://host[:port]` as the
// URL parser prints it. Anything else — another scheme, a path, `user@`, a trailing dot — never matches an entry.
const MAX_ORIGIN = 255;
const HOST = /^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*|\[[0-9a-f:.]+\])$/;

export const canonicalVaultOrigin = (value: unknown): string | null => {
  if (typeof value !== 'string' || !value || value.length > MAX_ORIGIN) return null;
  const scheme = /^https:\/\//i.exec(value);
  if (!scheme) return null;
  const rest = value.slice(scheme[0].length).replace(/\/$/, '');
  // eslint-disable-next-line no-irregular-whitespace -- the no-break space is one of the characters the range lets in
  if (!rest || /[^\x21-\x7e -￿]|[/?#@\\]/.test(rest)) return null;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.port === '0') return null;
  if (!HOST.test(url.hostname) || url.origin.length > MAX_ORIGIN) return null;
  return url.origin;
};

/** Exact scheme, host and port: a subdomain, a parent domain or another port is another site. */
export const sameVaultOrigin = (a: unknown, b: unknown): boolean => {
  const left = canonicalVaultOrigin(a);
  return left !== null && left === canonicalVaultOrigin(b);
};
