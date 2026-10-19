export const SESSION_COOKIE = 'session-token';
export const MAX_AGE = 7 * 86400;
export const signSessionToken = async () => 'good';
export const verifySessionToken = async (token) => (token === 'good' ? { exp: Math.floor(Date.now() / 1000) + MAX_AGE } : null);
export const buildCookieHeader = (token) => `session-token=${token}`;
export const verifyRequestSession = async (cookie) => String(cookie ?? '').includes('session-token=good');
