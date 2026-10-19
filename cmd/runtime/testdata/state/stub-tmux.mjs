// tmux as the generator lays it out: globalThis.__golden.sessions maps a session name to its working folder. Like the
// real module, a name that is not a string throws where the real one called name.startsWith outside a try, and reads
// as no session where the real one caught it.
const sessions = () => globalThis.__golden.sessions;
const name = (n) => { n.startsWith('pt-'); return n; };
export const hasSession = async (n) => Object.hasOwn(sessions(), name(n));
export const getSessionPanePid = async (n) => { try { name(n); } catch { return null; } return Object.hasOwn(sessions(), n) ? 1 : null; };
export const getSessionCwd = async (n) => { try { name(n); } catch { return null; } return sessions()[n] ?? null; };
export const socketOf = async () => 'test';
export const listSessions = async () => Object.keys(sessions());
export const getAllPanesInfo = async () => new Map();
