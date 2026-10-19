// The tabs of the fixture layouts, in workspace order.
export const findTabById = async (tabId) => {
  if (!tabId) return null;
  const tab = globalThis.__golden.tabs.find((t) => t.id === tabId);
  return tab ? { workspace: { id: 'ws-a' }, tab } : null;
};
export const listWorkspaceTabs = async () => globalThis.__golden.tabs.map((tab) => ({ workspace: { id: 'ws-a' }, tab }));
