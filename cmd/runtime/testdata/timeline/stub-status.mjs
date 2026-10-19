// The status keeper as the generator's list of agent tabs.
const manager = { agentSources: () => globalThis.__golden.tabs };
export const getStatusManager = () => manager;
