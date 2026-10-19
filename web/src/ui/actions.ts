import { agents, layout as layoutApi } from '../api/endpoints';
import type { Layout, PanelType, Provider } from '../api/types';
import { findPane, findTab } from '../lib/layout-tree';
import { applyLayout, refreshLayout, reportError } from '../stores/runtime';

// Layout changes from the page: each route answers with the new layout (or nothing), which is applied at once; the
// sync socket's event refetches it as well.

export async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
  try {
    return await fn();
  } catch (err) {
    reportError(err);
    return undefined;
  }
}

async function withLayout(workspaceId: string, fn: () => Promise<Layout | null | undefined>) {
  const next = await run(fn);
  if (next && typeof next === 'object' && 'root' in next) applyLayout(workspaceId, next);
  else await refreshLayout(workspaceId);
}

export const actions = {
  /** The new pane's terminal starts in the folder of the pane it splits. */
  split: (w: string, paneId: string, orientation: 'horizontal' | 'vertical', cwd?: string) =>
    withLayout(w, () => layoutApi.split(w, paneId, orientation, cwd)),
  closePane: (w: string, paneId: string) => withLayout(w, () => layoutApi.closePane(w, paneId)),
  focusPane: (w: string, paneId: string) => withLayout(w, () => layoutApi.setActivePane(w, paneId)),
  selectTab: (w: string, paneId: string, tabId: string) =>
    withLayout(w, () => layoutApi.setActiveTab(w, paneId, tabId)),
  closeTab: (w: string, paneId: string, tabId: string) =>
    withLayout(w, async () => {
      await layoutApi.closeTab(w, paneId, tabId);
      return null;
    }),
  reorder: (w: string, paneId: string, ids: string[]) =>
    withLayout(w, () => layoutApi.reorderTabs(w, paneId, ids)),
  move: (w: string, from: string, tabId: string, to: string, index: number) =>
    withLayout(w, () => layoutApi.moveTab(w, from, tabId, to, index)),
  resize: (w: string, path: number[], ratio: number) => withLayout(w, () => layoutApi.resize(w, path, ratio)),
  equalize: (w: string) => withLayout(w, () => layoutApi.equalize(w)),
  rename: (w: string, paneId: string, tabId: string, name: string) =>
    withLayout(w, () => layoutApi.renameTab(w, paneId, tabId, name)),

  restart: (w: string, paneId: string, tabId: string) => run(() => layoutApi.restartTab(w, paneId, tabId)),

  newTerminal: (w: string, paneId: string, panelType: PanelType = 'terminal') =>
    withLayout(w, async () => {
      await layoutApi.createTab(w, paneId, { panelType });
      return null;
    }),

  /**
   * An agent with the runtime's hooks. The only page route that starts one puts it in the workspace's first pane, so it
   * is moved to the pane it was asked for afterwards.
   */
  newAgent: async (w: string, paneId: string, provider: Provider, prompt?: string) => {
    const conv = await run(() => agents.start(provider, w, prompt));
    if (!conv) return;
    const now = await run(() => layoutApi.get(w));
    if (!now) return;
    applyLayout(w, now);
    const at = findTab(now.root, conv.id);
    const target = findPane(now.root, paneId);
    if (at && target && at.pane.id !== paneId) {
      await withLayout(w, () => layoutApi.moveTab(w, at.pane.id, conv.id, paneId, target.tabs.length));
    }
  },
};
