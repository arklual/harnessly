import type { ITab, TLayoutNode } from '@/types/terminal';

/** Every tab of a pane tree, pane by pane. */
export const collectAllTabs = (node: TLayoutNode): ITab[] =>
  node.type === 'pane'
    ? node.tabs
    : [...collectAllTabs(node.children[0]), ...collectAllTabs(node.children[1])];
