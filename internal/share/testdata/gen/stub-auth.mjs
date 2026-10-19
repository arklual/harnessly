// Sessions by cookie value: "good" a page session, "person" one signed for a person, "password" a password login.
export const SESSION_COOKIE = 'session-token';
export const MAX_AGE = 7 * 86400;
const SUBS = { good: undefined, person: 'alice@example.com', password: 'user', number: 7 };
export const signSessionToken = async () => 'good';
export const buildCookieHeader = (token) => `session-token=${token}`;
export const verifySessionToken = async (token) =>
  token in SUBS ? { exp: Math.floor(Date.now() / 1000) + MAX_AGE, ...(SUBS[token] === undefined ? {} : { sub: SUBS[token] }) } : null;
export const extractCookie = (header, name) => {
  for (const part of header.split(';')) {
    const trimmed = part.trim();
    const eq = trimmed.indexOf('=');
    if (eq !== -1 && trimmed.slice(0, eq) === name) return trimmed.slice(eq + 1);
  }
  return undefined;
};
export const verifyRequestSession = async (cookie) => !!(await verifySessionToken(extractCookie(cookie ?? '', SESSION_COOKIE) ?? ''));
