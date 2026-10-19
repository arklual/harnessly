// The tab keeper: what it was told, and which tabs work.
import { world, record } from './stub-world.mjs';

const manager = {
  notifyLastUserMessage: (session, message) => record('notifyLastUserMessage', session, message),
  markAgentLaunch: (tabId) => record('markAgentLaunch', tabId),
  markAgentGone: (tabId) => record('markAgentGone', tabId),
  isSessionBusy: (session) => world().busy.has(session),
};
export const getStatusManager = () => manager;
