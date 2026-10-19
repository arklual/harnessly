// The tab keeper: the clients and agent sources the golden sets on globalThis.__golden.
const g = () => globalThis.__golden;
export const getStatusManager = () => ({
  getAllForClient: () => g().clients,
  agentSources: () => g().sources,
});
