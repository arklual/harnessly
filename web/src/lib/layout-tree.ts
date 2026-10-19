import type { LayoutNode, PaneNode, Tab } from '../api/types';

/** The panes of a layout in reading order. */
export function panesOf(node: LayoutNode): PaneNode[] {
  return node.type === 'pane' ? [node] : [...panesOf(node.children[0]), ...panesOf(node.children[1])];
}

export function findPane(node: LayoutNode, paneId: string): PaneNode | undefined {
  return panesOf(node).find((p) => p.id === paneId);
}

export function findTab(node: LayoutNode, tabId: string): { pane: PaneNode; tab: Tab } | undefined {
  for (const pane of panesOf(node)) {
    const tab = pane.tabs.find((t) => t.id === tabId);
    if (tab) return { pane, tab };
  }
  return undefined;
}

/** The pane's tabs in the order the runtime keeps them. */
export function sortedTabs(pane: PaneNode): Tab[] {
  return [...pane.tabs].sort((a, b) => a.order - b.order);
}

/** A tab's id list with one tab moved to a new index (the reorder route's body). */
export function moveId(ids: string[], id: string, to: number): string[] {
  const rest = ids.filter((x) => x !== id);
  const at = Math.max(0, Math.min(to, rest.length));
  return [...rest.slice(0, at), id, ...rest.slice(at)];
}

/** Clamps a divider drag to a ratio that leaves both sides usable. */
export function clampRatio(ratio: number, min = 10): number {
  return Math.round(Math.max(min, Math.min(100 - min, ratio)) * 100) / 100;
}

export function isAgentTab(tab: Tab): boolean {
  return tab.panelType === 'claude-code' || tab.panelType === 'codex-cli';
}

export function tabLabel(tab: Tab): string {
  if (tab.name) return tab.name;
  if (tab.panelType === 'claude-code') return 'Claude Code';
  if (tab.panelType === 'codex-cli') return 'Codex';
  return tab.title || tab.lastCommand || 'Terminal';
}
