interface AgentLayout {
  focusLoaded?: (workspaceId: string, tabId: string) => boolean;
  setPending: (workspaceId: string, tabId: string) => void;
  load: (workspaceId: string) => Promise<void>;
  focus: (tabId: string) => boolean;
}
export async function loadAndFocusAgent(
  layout: AgentLayout,
  workspaceId: string,
  tabId: string,
  isCurrent: () => boolean,
): Promise<boolean> {
  if (!isCurrent()) return false;
  if (layout.focusLoaded?.(workspaceId, tabId)) return true;
  layout.setPending(workspaceId, tabId);
  await layout.load(workspaceId);
  if (!isCurrent()) return false;
  return layout.focus(tabId);
}
