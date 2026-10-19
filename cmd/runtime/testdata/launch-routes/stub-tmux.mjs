// tmux as a recorded set of sessions: what is created and typed lands in __golden.calls.
import fs from 'node:fs/promises';
import path from 'node:path';

export const SESSION_PREFIX = 'tk-';
export const LEGACY_SESSION_PREFIX = 'pt-';
export const NEW_SESSION_PREFIX = SESSION_PREFIX;
const g = () => globalThis.__golden;
const t = () => g().tmux;
const rec = (...call) => g().calls.push(call);

export const workspaceSessionName = (ws, pane, tab) => `${NEW_SESSION_PREFIX}${ws}-${pane}-${tab}`;
export const parseSessionName = (name) => {
  const m = /^(?:tk-|pt-)(ws-[A-Za-z0-9_-]+?)-(pane-[A-Za-z0-9_-]+?)-(tab-[A-Za-z0-9_-]+)$/.exec(name);
  return m ? { wsId: m[1], paneId: m[2], tabId: m[3] } : null;
};
export const createSession = async (name, cols, rows, cwd) => { rec('create', name, String(cols), String(rows), cwd ?? ''); t().sessions.add(name); };
export const killSession = async (name) => { rec('kill', name); t().sessions.delete(name); };
export const hasSession = async (name) => t().sessions.has(name);
export const sendKeys = async (name, text) => { rec('send', name, text); };
export const sendKeysSeparated = async (name, text) => { rec('sendSeparated', name, text); };
export const sendBracketedPaste = async (name, text) => { rec('paste', name, text); };
export const listSessions = async () => [...t().sessions].sort();
export const getSessionCwd = async (name) => t().cwd[name] ?? null;
export const getSessionPanePid = async () => null;
export const getSessionsOfPane = async (pane, socket) => (t().panes[pane] ?? []).filter(([s]) => !socket || s === socket).map(([, n]) => n);
export const getPaneCurrentCommand = async (name) => t().commands[name] ?? 'bash';
export const getPaneDetailInfo = async (name) => ({ cwd: null, command: null, pid: null, width: null, height: null, sessionCreated: t().created[name] ?? null });
export const capturePaneContent = async () => 'line\n';
export const checkTerminalProcess = async () => ({ isSafe: true, processName: 'bash' });
export const exitCopyMode = async () => {};
export const applyConfig = async () => {};
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
