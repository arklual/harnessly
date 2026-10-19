// The layout store: the writes the timeline makes, and session names hs-<workspace>-<pane>-<tab>.
import { record } from './stub-world.mjs';

export const parseSessionName = (s) => {
  const m = /^hs-([^-]+)-([^-]+)-([^-]+)$/.exec(s);
  return m ? { wsId: m[1], paneId: m[2], tabId: m[3] } : null;
};
export const updateTabAgentSessionId = async (session, provider, sessionId) => record('agentSessionId', session, provider.id, sessionId ?? null);
export const updateTabAgentSummary = async (session, provider, summary) => record('agentSummary', session, provider.id, summary ?? null);
export const updateTabLastUserMessage = async (session, message) => record('lastUserMessage', session, message);
export const updateTabAgentState = async (session, provider, { sessionId, jsonlPath, summary }) =>
  record('agentState', session, provider.id, sessionId ?? null, jsonlPath ?? null, summary ?? null);
