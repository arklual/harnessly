// Session tokens of the golden run: "good" is a session without a subject, "sub:<who>" one naming <who>.
export const SESSION_COOKIE = 'session-token';
export const MAX_AGE = 7 * 86400;
export const signSessionToken = async () => 'good';
const exp = () => Math.floor(Date.now() / 1000) + MAX_AGE;
export const verifySessionToken = async (token) => {
  if (token === 'good') return { exp: exp() };
  if (typeof token === 'string' && token.startsWith('sub:')) return { exp: exp(), sub: token.slice(4) };
  return null;
};
export const buildCookieHeader = (token) => `session-token=${token}`;
export const extractCookie = (header, name) => {
  for (const part of String(header ?? '').split(';')) {
    const at = part.indexOf('=');
    if (at > 0 && part.slice(0, at).trim() === name) return part.slice(at + 1).trim();
  }
  return null;
};
export const verifyRequestSession = async (cookie) => !!(await verifySessionToken(extractCookie(cookie, SESSION_COOKIE)));
