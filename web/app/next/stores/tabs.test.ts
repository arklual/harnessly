// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  activateTab,
  activeTab,
  closeOtherTabs,
  closeRightTabs,
  closeTab,
  createTab,
  dedupeTabs,
  HOME_PATH,
  initTabs,
  moveTab,
  navigateTab,
  nthTab,
  openTab,
  OPEN,
  parseSaved,
  planOpen,
  patchTab,
  resetTabs,
  serialize,
  startTabs,
  stepTab,
  tabs,
  tabsSnapshot,
  TABS_KEY,
  useTabTitle,
  type ITabsState,
} from '@/next/stores/tabs';

const state = (...paths: string[]): ITabsState => {
  const list = paths.map((path, i) => createTab(path, `t${i + 1}`));
  return { tabs: list, active: list[0].id };
};
const paths = (s: ITabsState) => s.tabs.map((tab) => tab.path);

describe('tabs: open and navigate', () => {
  it('opens a tab right after the active one and switches to it', () => {
    const s = openTab(activateTab(state('/', '/c/tab-a', '/settings'), 't2'), '/designly', { id: 'n' });
    expect(paths(s)).toEqual(['/', '/c/tab-a', '/designly', '/settings']);
    expect(s.active).toBe('n');
  });

  it('opens «+» at the end, and a ⌘-click behind the active tab', () => {
    const end = openTab(state('/c/tab-a', '/c/tab-b'), '/', { at: 'end', id: 'n' });
    expect(paths(end)).toEqual(['/c/tab-a', '/c/tab-b', '/']);
    const behind = openTab(state('/c/tab-a', '/c/tab-b'), '/c/tab-c', { background: true });
    expect(paths(behind)).toEqual(['/c/tab-a', '/c/tab-c', '/c/tab-b']);
    expect(behind.active).toBe('t1');
  });

  it('navigates the active tab only, and a new place starts with no title and no scroll', () => {
    const s0 = patchTab(state('/c/tab-a', '/settings'), 't1', { title: 'A', scroll: { '.hx-feed': 300 } });
    const s = navigateTab(s0, '/designly/afisha');
    expect(paths(s)).toEqual(['/designly/afisha', '/settings']);
    expect(activeTab(s)).toMatchObject({ title: null, scroll: null });
    expect(navigateTab(s, '/designly/afisha')).toBe(s);
  });

  it('an accepted invitation leaves no token in the tab path or in what is kept', () => {
    const s = navigateTab(state('/designly/invite/tok_12345678'), '/designly/p1');
    expect(paths(s)).toEqual(['/designly/p1']);
    expect(serialize(s)).not.toContain('tok_12345678');
  });
});

describe('tabs: opening from a menu', () => {
  it('switches to the tab that already has the place', () => {
    const plan = planOpen(state('/designly/afisha', '/c/tab-a', '/settings'), '/c/tab-a');
    expect(plan.did).toBe(OPEN.focus);
    expect(plan.state.active).toBe('t2');
    expect(paths(plan.state)).toEqual(['/designly/afisha', '/c/tab-a', '/settings']);
  });

  it('opens a new place as a new active tab right after the current one, and never replaces it', () => {
    const plan = planOpen(activateTab(state('/designly/afisha', '/settings'), 't1'), '/c/tab-a', { id: 'n' });
    expect(plan.did).toBe(OPEN.create);
    expect(paths(plan.state)).toEqual(['/designly/afisha', '/c/tab-a', '/settings']);
    expect(plan.state.active).toBe('n');
  });

  it('keeps the active tab for a background open, and changes nothing for the place already on view', () => {
    const behind = planOpen(state('/a', '/b'), '/c', { background: true });
    expect(paths(behind.state)).toEqual(['/a', '/c', '/b']);
    expect(behind.state.active).toBe('t1');
    const known = planOpen(state('/a', '/b'), '/b', { background: true });
    expect(known.did).toBe(OPEN.keep);
    expect(known.state.active).toBe('t1');
    const same = state('/a', '/b');
    expect(planOpen(same, '/a').state).toBe(same);
  });

  it('opening a chat from a design tab leaves the design tab as it was', () => {
    const s = patchTab(state('/designly/afisha'), 't1', { title: 'Афиша', view: { pane: 1 } });
    const plan = planOpen(s, '/c/tab-a');
    expect(plan.state.tabs[0]).toBe(s.tabs[0]);
    expect(plan.state.tabs).toHaveLength(2);
  });
});

describe('tabs: close', () => {
  it('hands the active tab over to the right neighbour, at the end to the left one', () => {
    const middle = closeTab(activateTab(state('/a', '/b', '/c'), 't2'), 't2');
    expect(paths(middle)).toEqual(['/a', '/c']);
    expect(middle.active).toBe('t3');
    const last = closeTab(activateTab(state('/a', '/b', '/c'), 't3'), 't3');
    expect(last.active).toBe('t2');
  });

  it('closing a background tab keeps the active one', () => {
    const s = closeTab(state('/a', '/b'), 't2');
    expect(s.active).toBe('t1');
    expect(paths(s)).toEqual(['/a']);
  });

  it('closing the last tab leaves a home tab', () => {
    const s = closeTab(state('/c/tab-a'), 't1');
    expect(paths(s)).toEqual([HOME_PATH]);
    expect(s.active).toBe(s.tabs[0].id);
  });

  it('closes the others', () => {
    expect(paths(closeOtherTabs(state('/a', '/b', '/c'), 't2'))).toEqual(['/b']);
  });

  it('closes the tabs to the right, and the active one among them hands over to the one kept', () => {
    const three = state('/a', '/b', '/c');
    expect(paths(closeRightTabs(three, 't1'))).toEqual(['/a']);
    expect(closeRightTabs(three, 't1').active).toBe('t1');
    expect(closeRightTabs(activateTab(three, 't2'), 't3').tabs).toHaveLength(3);
    const kept = closeRightTabs(activateTab(three, 't1'), 't2');
    expect(paths(kept)).toEqual(['/a', '/b']);
    expect(kept.active).toBe('t1');
    expect(closeRightTabs(three, 'nope')).toBe(three);
  });
});

describe('tabs: reorder and switch', () => {
  it('moves a tab to another place, clamped to the strip', () => {
    expect(paths(moveTab(state('/a', '/b', '/c'), 't1', 2))).toEqual(['/b', '/c', '/a']);
    expect(paths(moveTab(state('/a', '/b', '/c'), 't3', -4))).toEqual(['/c', '/a', '/b']);
    const s = state('/a');
    expect(moveTab(s, 't1', 0)).toBe(s);
  });

  it('steps round the ends and picks by number, 9 the last one', () => {
    const s = state('/a', '/b', '/c');
    expect(stepTab(s, -1).active).toBe('t3');
    expect(stepTab(activateTab(s, 't3'), 1).active).toBe('t1');
    expect(nthTab(s, 2).active).toBe('t2');
    expect(nthTab(s, 9).active).toBe('t3');
    expect(nthTab(s, 5)).toBe(s);
  });
});

describe('tabs: restore', () => {
  const saved = activateTab(state('/c/tab-a', '/designly/afisha'), 't2');

  it('keeps the kept tabs when the address is the active one', () => {
    expect(initTabs(saved, '/designly/afisha')).toEqual({ state: saved, go: null });
  });

  it('a bare / goes back to the active tab, unless asked not to', () => {
    expect(initTabs(saved, HOME_PATH).go).toBe('/designly/afisha');
    const plain = initTabs(saved, HOME_PATH, false);
    expect(plain.go).toBeNull();
    expect(activeTab(plain.state).path).toBe(HOME_PATH);
  });

  it('an address another tab has switches to it; a new one opens a tab', () => {
    expect(initTabs(saved, '/c/tab-a').state.active).toBe('t1');
    const fresh = initTabs(saved, '/settings');
    expect(paths(fresh.state)).toEqual(['/c/tab-a', '/designly/afisha', '/settings']);
    expect(activeTab(fresh.state).path).toBe('/settings');
  });

  it('nothing kept: one tab of the address', () => {
    const { state: s } = initTabs(null, '/c/tab-a');
    expect(paths(s)).toEqual(['/c/tab-a']);
  });
});

describe('tabs: persist', () => {
  it('round-trips through its JSON and refuses what is not its own', () => {
    const s = patchTab(state('/c/tab-a', '/'), 't1', { title: 'A', view: { 'site:x:tab': 'p1' } });
    expect(parseSaved(serialize(s))).toEqual(s);
    expect(parseSaved(null)).toBeNull();
    expect(parseSaved('not json')).toBeNull();
    expect(parseSaved(JSON.stringify({ v: 99, tabs: [] }))).toBeNull();
    expect(
      parseSaved(JSON.stringify({ v: 1, tabs: [{ id: 1 }, { id: 'a', path: '/x' }], active: 'zz' })),
    ).toEqual({
      tabs: [{ id: 'a', path: '/x', title: null, scroll: null, view: null }],
      active: 'a',
    });
  });

  describe('the store', () => {
    beforeEach(() => {
      vi.useFakeTimers();
      localStorage.clear();
      resetTabs();
    });
    afterEach(() => {
      resetTabs();
      vi.useRealTimers();
    });

    it('writes a moment after a change and comes back after a reload', () => {
      expect(startTabs('/c/tab-a', true)).toBeNull();
      tabs.open('/designly/afisha');
      tabs.patch(tabsSnapshot().active, { title: 'Afisha' });
      expect(localStorage.getItem(TABS_KEY)).toBeNull();
      vi.advanceTimersByTime(400);
      expect(parseSaved(localStorage.getItem(TABS_KEY))?.tabs.map((tab) => tab.path)).toEqual([
        '/c/tab-a',
        '/designly/afisha',
      ]);

      resetTabs();
      expect(startTabs(HOME_PATH, true)).toBe('/designly/afisha');
      expect(activeTab(tabsSnapshot()).title).toBe('Afisha');
      // Started once: a second start changes nothing.
      expect(startTabs('/settings', true)).toBeNull();
      expect(tabsSnapshot().tabs).toHaveLength(2);
    });

    it('closing every tab leaves home, and the order is kept', () => {
      startTabs('/a', true);
      tabs.open('/b', { at: 'end' });
      tabs.move(tabsSnapshot().active, 0);
      expect(paths(tabsSnapshot())).toEqual(['/b', '/a']);
      for (const tab of [...tabsSnapshot().tabs]) tabs.close(tab.id);
      expect(paths(tabsSnapshot())).toEqual([HOME_PATH]);
    });

    it('names the tab that takes the path after the title came', () => {
      startTabs(HOME_PATH, true);
      const { unmount } = renderHook(() => useTabTitle('/designly/afisha', 'Афиша'));
      expect(activeTab(tabsSnapshot()).title).toBeNull();
      act(() => tabs.navigate('/designly/afisha'));
      expect(activeTab(tabsSnapshot()).title).toBe('Афиша');
      unmount();
    });
  });
});

describe('tabs: one tab per place', () => {
  it('opening a chat that is open in a tab (any spelling) switches to it', () => {
    const s = state('/designly', '/c/tab-a', '/settings');
    for (const path of ['/c/tab-a/', '/c/tab-a?pane=1', '/c/tab-a#x']) {
      const plan = planOpen(s, path);
      expect(plan.did).toBe(OPEN.focus);
      expect(plan.state.active).toBe('t2');
      expect(plan.state.tabs).toHaveLength(3);
    }
  });

  it('the same design is one tab, another board shows as asked; another chat is another tab', () => {
    const s = state('/c/x', '/designly/afisha?page=1');
    const plan = planOpen(s, '/design/afisha?page=2');
    expect(plan.did).toBe(OPEN.focus);
    expect(plan.state.tabs[1].path).toBe('/design/afisha?page=2');
    expect(plan.state.tabs).toHaveLength(2);
    const withChat = planOpen(s, '/designly/afisha?chat=tab-a', { id: 'n' });
    expect(withChat.did).toBe(OPEN.create);
    expect(planOpen(withChat.state, '/designly/afisha?chat=tab-a&page=3').did).toBe(OPEN.keep);
  });

  it('settings sections share a tab', () => {
    const plan = planOpen(state('/', '/settings/general'), '/settings/claude');
    expect(plan.did).toBe(OPEN.focus);
    expect(plan.state.tabs.map((tab) => tab.path)).toEqual(['/', '/settings/claude']);
  });

  it('navigating to a place open in another tab switches to it and leaves the active tab as it was', () => {
    const s0 = patchTab(state('/c/tab-a', '/c/tab-b'), 't1', { title: 'A', scroll: { '.hx-feed': 5 } });
    const s = navigateTab(s0, '/c/tab-b/');
    expect(s.active).toBe('t2');
    expect(s.tabs[0]).toBe(s0.tabs[0]);
    expect(s.tabs[1].path).toBe('/c/tab-b/');
    expect(s.tabs).toHaveLength(2);
  });

  it('navigating inside the same place keeps the tab, its title and its scroll', () => {
    const s0 = patchTab(state('/designly/afisha', '/settings'), 't1', {
      title: 'Афиша',
      scroll: { '.hx-dl': 9 },
    });
    const s = navigateTab(s0, '/designly/afisha?board=2');
    expect(s.tabs).toHaveLength(2);
    expect(activeTab(s)).toMatchObject({
      path: '/designly/afisha?board=2',
      title: 'Афиша',
      scroll: { '.hx-dl': 9 },
    });
  });

  it('collapses duplicates on load, keeping the first; the active twin hands over to it', () => {
    const list = ['/c/tab-a', '/settings/general', '/c/tab-a/', '/settings/claude', '/designly'].map(
      (path, i) => createTab(path, `t${i + 1}`),
    );
    const out = dedupeTabs({ tabs: list, active: 't3' });
    expect(out.tabs.map((tab) => tab.id)).toEqual(['t1', 't2', 't5']);
    expect(out.active).toBe('t1');
    const parsed = parseSaved(JSON.stringify({ v: 1, tabs: list, active: 't4' }));
    expect(parsed?.tabs.map((tab) => tab.id)).toEqual(['t1', 't2', 't5']);
    expect(parsed?.active).toBe('t2');
    const clean = state('/a', '/b');
    expect(dedupeTabs(clean)).toBe(clean);
  });

  it('a reload on a place with another query keeps the one tab', () => {
    const saved = activateTab(state('/c/tab-a', '/designly/afisha'), 't2');
    const init = initTabs(saved, '/designly/afisha?page=4');
    expect(init.state.tabs).toHaveLength(2);
    expect(activeTab(init.state).path).toBe('/designly/afisha?page=4');
  });
});
