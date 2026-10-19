// tmux as a set of session names.
import fs from 'node:fs/promises';
import path from 'node:path';

export const SESSION_PREFIX = 'tk-';
export const LEGACY_SESSION_PREFIX = 'pt-';
export const NEW_SESSION_PREFIX = SESSION_PREFIX;
const t = () => globalThis.__golden.tmux;

export const workspaceSessionName = (ws, pane, tab) => `${NEW_SESSION_PREFIX}${ws}-${pane}-${tab}`;
export const createSession = async (name) => { t().sessions.add(name); };
export const killSession = async (name) => { t().sessions.delete(name); };
export const hasSession = async (name) => t().sessions.has(name);
export const sendKeys = async () => {};
export const sendBracketedPaste = async (name, text) => { globalThis.__golden.pasted.push([name, text]); };
export const listSessions = async () => [...t().sessions].sort();
export const getSessionCwd = async () => null;
export const getPaneCurrentCommand = async () => 'bash';
export const capturePaneContent = async () => 'line\n';
export const checkTerminalProcess = async () => ({ isSafe: true, processName: 'bash' });
export const resolveExistingDir = async (preferred) => {
  const fallback = process.env.HOME || '/';
  if (!preferred) return fallback;
  let candidate = preferred;
  for (;;) {
    try {
      if ((await fs.stat(candidate)).isDirectory()) return candidate;
    } catch {}
    const parent = path.dirname(candidate);
    if (parent === candidate) return fallback;
    candidate = parent;
  }
};
