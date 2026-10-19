// A page session is the cookie session-token=good; the machine agent's is session-token=agent.
export const SESSION_COOKIE = 'session-token';
export const MAX_AGE = 7 * 86400;
export const signSessionToken = async () => 'signed';
export const verifySessionToken = async (token) => (token === 'good' || token === 'agent' ? { exp: Math.floor(Date.now() / 1000) + MAX_AGE, sub: token === 'agent' ? 'machine-agent' : 'user' } : null);
export const buildCookieHeader = (token, secure) => `session-token=${token}; HttpOnly; SameSite=Lax; Path=/; Max-Age=${MAX_AGE}${secure ? '; Secure' : ''}`;
export const clearCookieHeader = () => 'session-token=; HttpOnly; SameSite=Lax; Path=/; Max-Age=0';
export const isSecureRequest = (req) => req.headers['x-forwarded-proto'] === 'https';
export const verifyRequestSession = async (header) => /(?:^|;\s*)session-token=(good|agent)(?:;|$)/.test(String(header ?? ''));
export const verifyMachineAgentRequest = async (header) => /(?:^|;\s*)session-token=agent(?:;|$)/.test(String(header ?? ''));
