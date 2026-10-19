// tmux as a set of session names; what the stores asked of it is recorded.
import fs from 'node:fs/promises';
import path from 'node:path';

export const SESSION_PREFIX = 'tk-';
export const LEGACY_SESSION_PREFIX = 'pt-';
export const NEW_SESSION_PREFIX = SESSION_PREFIX;

const t = () => globalThis.__golden.tmux;
const note = (...call) => globalThis.__golden.calls.push(call);

export const workspaceSessionName = (ws, pane, tab) => `${NEW_SESSION_PREFIX}${ws}-${pane}-${tab}`;
export const createSession = async (name, cols, rows, cwd) => { note('create', name, cols, rows, cwd ?? ''); t().sessions.add(name); };
export const killSession = async (name) => { note('kill', name); t().sessions.delete(name); };
export const hasSession = async (name) => t().sessions.has(name);
export const sendKeys = async (name, command) => { note('send', name, command); };
export const listSessions = async () => [...t().sessions].sort();
export const getSessionCwd = async () => t().cwd;
export const getPaneCurrentCommand = async () => t().command;
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
