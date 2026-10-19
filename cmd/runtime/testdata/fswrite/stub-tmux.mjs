// tmux as the generator lays it out: globalThis.__golden.sessions maps a session name to its working folder.
export const hasSession = async (name) => Object.hasOwn(globalThis.__golden.sessions, name);
export const getSessionCwd = async (name) => globalThis.__golden.sessions[name] ?? null;
export const getAllPanesInfo = async () => new Map();
