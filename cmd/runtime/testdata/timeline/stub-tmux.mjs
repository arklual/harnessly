// A tmux session's working folder from the generator's table.
export const getSessionCwd = async (name) => globalThis.__golden.cwds[name] ?? null;
