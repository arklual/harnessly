// tmux as a table of panes.
import { world, record } from './stub-world.mjs';

const pane = (s) => world().panes[s];
export const getSessionPanePid = async (s) => pane(s)?.pid ?? null;
export const getPaneTitle = async (s) => pane(s)?.title || null;
export const getSessionCwd = async (s) => pane(s)?.cwd ?? null;
export const capturePaneContent = async (s) => pane(s)?.screen ?? null;
export const checkTerminalProcess = async (s) => {
  const command = pane(s)?.command;
  if (!command) return { isSafe: false, processName: 'unknown' };
  return { isSafe: ['bash', 'zsh', 'fish', 'sh', 'dash'].includes(command), processName: command };
};
export const sendKeys = async (s, keys) => { record('sendKeys', s, keys); };
