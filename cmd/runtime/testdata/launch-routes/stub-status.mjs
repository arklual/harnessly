// The status keeper: tabs registered and launches marked land in __golden.calls; its agent tabs are __golden.sources.
const rec = (...call) => globalThis.__golden.calls.push(call);
const manager = {
  getAllForClient: () => ({}),
  registerTab: (id, entry) => rec('register', id, JSON.stringify(entry)),
  removeTab: () => {},
  markAgentLaunch: (id, opts) => rec('launch', id, opts?.resetAgentSession ? 'reset' : ''),
  agentSources: () => globalThis.__golden.sources,
  rescan: async () => {},
};
export const getStatusManager = () => manager;
