import { describe, expect, it } from 'vitest';

import type { LayoutNode, Tab } from '../api/types';
import { clampRatio, findPane, findTab, moveId, panesOf, sortedTabs, tabLabel } from './layout-tree';

const tab = (id: string, order: number, extra: Partial<Tab> = {}): Tab => ({
  id,
  order,
  name: '',
  sessionName: `hs-ws-pane-${id}`,
  ...extra,
});

const tree: LayoutNode = {
  type: 'split',
  orientation: 'horizontal',
  ratio: 50,
  children: [
    { type: 'pane', id: 'pane-a', tabs: [tab('t2', 1), tab('t1', 0)], activeTabId: 't1' },
    {
      type: 'split',
      orientation: 'vertical',
      ratio: 40,
      children: [
        { type: 'pane', id: 'pane-b', tabs: [tab('t3', 0, { panelType: 'claude-code' })], activeTabId: 't3' },
        { type: 'pane', id: 'pane-c', tabs: [], activeTabId: null },
      ],
    },
  ],
};

describe('layout tree', () => {
  it('lists panes in reading order', () => {
    expect(panesOf(tree).map((p) => p.id)).toEqual(['pane-a', 'pane-b', 'pane-c']);
  });

  it('finds panes and tabs', () => {
    expect(findPane(tree, 'pane-c')?.tabs).toEqual([]);
    expect(findTab(tree, 't3')?.pane.id).toBe('pane-b');
    expect(findTab(tree, 'nope')).toBeUndefined();
  });

  it('sorts tabs by order', () => {
    expect(sortedTabs(findPane(tree, 'pane-a')!).map((t) => t.id)).toEqual(['t1', 't2']);
  });

  it('moves an id within a list', () => {
    expect(moveId(['a', 'b', 'c'], 'a', 2)).toEqual(['b', 'c', 'a']);
    expect(moveId(['a', 'b', 'c'], 'c', 0)).toEqual(['c', 'a', 'b']);
    expect(moveId(['a', 'b', 'c'], 'b', 99)).toEqual(['a', 'c', 'b']);
  });

  it('clamps ratios', () => {
    expect(clampRatio(3)).toBe(10);
    expect(clampRatio(97.333)).toBe(90);
    expect(clampRatio(33.3333)).toBe(33.33);
  });

  it('names tabs', () => {
    expect(tabLabel(tab('x', 0, { panelType: 'claude-code' }))).toBe('Claude Code');
    expect(tabLabel(tab('x', 0, { name: 'Build' }))).toBe('Build');
    expect(tabLabel(tab('x', 0, { lastCommand: 'make' }))).toBe('make');
  });
});
