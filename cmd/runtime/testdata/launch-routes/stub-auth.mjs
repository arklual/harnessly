// A page session is the cookie session-token=good; the machine agent's is session-token=agent over loopback.
export const SESSION_COOKIE = 'session-token';
export const MAX_AGE = 7 * 86400;
export const signSessionToken = async () => 'good';
export const verifySessionToken = async (token) => (token === 'good' || token === 'agent' ? { exp: Math.floor(Date.now() / 1000) + MAX_AGE } : null);
export const buildCookieHeader = (token) => `session-token=${token}`;
const cookie = (header) => /(?:^|;\s*)session-token=([^;]*)/.exec(String(header ?? ''))?.[1];
export const verifyRequestSession = async (header) => ['good', 'agent'].includes(cookie(header));
export const verifyMachineAgentRequest = async (header, remote) => ['127.0.0.1', '::1', '::ffff:127.0.0.1'].includes(remote) && cookie(header) === 'agent';
