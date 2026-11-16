import { describe, expect, it } from 'vitest';
import {
  NAV_ITEM,
  navLayout,
  navSettingsOf,
  withHidden,
  withPlaced,
  withRaised,
} from '@/next/features/sidebar/nav-layout';

const ITEMS = [NAV_ITEM.newChat, NAV_ITEM.search, NAV_ITEM.designly, NAV_ITEM.apps];
const EMPTY = { order: [], hidden: [], apps: [] };

describe('navLayout: the sidebar menu as the person laid it out (HRN-1484)', () => {
  it('by default: the items as they were, the boards and the documents first under «Приложения»', () => {
    const layout = navLayout(EMPTY, ITEMS, ['review', 'acme.notes', 'harnessly.docs', 'harnessly.boards']);
    expect(layout.top).toEqual(['newChat', 'search', 'designly', 'apps']);
    expect(layout.apps).toEqual(['harnessly.boards', 'harnessly.docs', 'review', 'acme.notes']);
  });
  it('keeps the saved order, raises an app, skips what is gone, puts a new item after its neighbour', () => {
    const nav = {
      order: ['apps', 'app:harnessly.docs', 'newChat', 'app:gone'],
      hidden: ['search'],
      apps: ['acme.notes'],
    };
    const layout = navLayout(nav, ITEMS, ['harnessly.docs', 'acme.notes', 'review']);
    expect(layout.top).toEqual(['apps', 'app:harnessly.docs', 'newChat', 'search', 'designly']);
    expect(layout.apps).toEqual(['acme.notes', 'review']);
    expect(layout.hidden.has('search')).toBe(true);
  });
  it('raises and lowers an app, hides and shows, and writes it all back', () => {
    let layout = navLayout(EMPTY, ITEMS, ['harnessly.boards', 'harnessly.docs']);
    layout = withRaised(layout, 'harnessly.docs', true);
    expect(layout.top.at(-1)).toBe('app:harnessly.docs');
    expect(layout.apps).toEqual(['harnessly.boards']);
    layout = withHidden(layout, 'search', true);
    expect(navSettingsOf(layout)).toEqual({
      order: ['newChat', 'search', 'designly', 'apps', 'app:harnessly.docs'],
      hidden: ['search'],
      apps: ['harnessly.boards'],
    });
    layout = withRaised(layout, 'harnessly.docs', false);
    expect(layout.apps).toEqual(['harnessly.docs', 'harnessly.boards']);
  });
  it('carries an app up to the main buttons and back by hand (HRN-1512)', () => {
    let layout = navLayout(EMPTY, ITEMS, ['harnessly.boards', 'harnessly.docs', 'acme.notes']);
    const up = withPlaced(layout, 'app:acme.notes', 'search', true);
    expect(up?.top).toEqual(['newChat', 'search', 'app:acme.notes', 'designly', 'apps']);
    expect(up?.apps).toEqual(['harnessly.boards', 'harnessly.docs']);
    layout = up ?? layout;
    const down = withPlaced(layout, 'app:acme.notes', 'app:harnessly.boards', false);
    expect(down?.top).toEqual(['newChat', 'search', 'designly', 'apps']);
    expect(down?.apps).toEqual(['acme.notes', 'harnessly.boards', 'harnessly.docs']);
    expect(withPlaced(layout, 'search', 'app:harnessly.docs', false)).toBeNull();
    expect(withPlaced(layout, 'search', 'newChat', true)).toBeNull();
    expect(withPlaced(layout, 'newChat', 'designly', true)?.top).toEqual([
      'search',
      'app:acme.notes',
      'designly',
      'newChat',
      'apps',
    ]);
  });
});
