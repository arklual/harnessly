// The open workspace's pane tree (GET /api/layout) and the tab in focus.
import { create } from 'zustand';
import { layout as api } from '@/runtime/endpoints';
import type { ILayoutData, IPaneNode, TLayoutNode } from '@/types/terminal';

export const collectPanes = (node: TLayoutNode): IPaneNode[] =>
  node.type === 'pane' ? [node] : [...collectPanes(node.children[0]), ...collectPanes(node.children[1])];

interface ILayoutState {
  workspaceId: string | null;
  layout: ILayoutData | null;
  /** A tab the page wants focused as soon as the layout that has it arrives. */
  pendingFocusTabId: string | null;
  setWorkspaceId: (id: string) => void;
  fetchLayout: (workspaceId: string, quiet?: boolean) => Promise<void>;
  setLayout: (layout: ILayoutData) => void;
  focusTab: (tabId: string) => boolean;
}

export const useLayoutStore = create<ILayoutState>((set, get) => ({
  workspaceId: null,
  layout: null,
  pendingFocusTabId: null,
  setWorkspaceId: (id) => {
    if (get().workspaceId !== id) set({ workspaceId: id, layout: null });
  },
  fetchLayout: async (workspaceId) => {
    try {
      const data = (await api.get(workspaceId)) as unknown as ILayoutData;
      if (get().workspaceId === null || get().workspaceId === workspaceId) {
        set({ workspaceId, layout: data });
        const pending = get().pendingFocusTabId;
        if (pending && get().focusTab(pending)) set({ pendingFocusTabId: null });
      }
    } catch {
      // the next sync message or page load tries again
    }
  },
  setLayout: (layout) => set({ layout }),
  focusTab: (tabId) => {
    const { layout, workspaceId } = get();
    if (!layout || !workspaceId) return false;
    const pane = collectPanes(layout.root).find((p) => p.tabs.some((t) => t.id === tabId));
    if (!pane) return false;
    if (pane.activeTabId !== tabId || layout.activePaneId !== pane.id) {
      const next: ILayoutData = {
        ...layout,
        activePaneId: pane.id,
        root: mapPane(layout.root, pane.id, (p) => ({ ...p, activeTabId: tabId })),
      };
      set({ layout: next });
      void api.setActiveTab(workspaceId, pane.id, tabId).catch(() => {});
    }
    return true;
  },
}));

const mapPane = (node: TLayoutNode, id: string, fn: (p: IPaneNode) => IPaneNode): TLayoutNode =>
  node.type === 'pane'
    ? node.id === id
      ? fn(node)
      : node
    : { ...node, children: [mapPane(node.children[0], id, fn), mapPane(node.children[1], id, fn)] };

export default useLayoutStore;
