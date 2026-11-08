import { describe, expect, it } from 'vitest';
import type { IDesignCard } from '@/next/features/designly/designly-home';
import {
  NO_FILTERS,
  RECENT_MS,
  SORT,
  childrenOf,
  folderChoices,
  isNarrowed,
  matchesFilters,
  projectsIn,
  ROUTE,
  routeToCard,
  sortCards,
  systemsOf,
} from '@/next/features/designly/library-model';

const NOW = 1_800_000_000_000;
const card = (over: Partial<IDesignCard>): IDesignCard => ({
  id: 'a',
  title: 'Alpha',
  boards: 1,
  updatedAt: NOW,
  image: null,
  kind: 'site',
  pinned: false,
  drawing: false,
  owner: null,
  ...over,
});

describe('library filters', () => {
  it('combine: kind, recent and system must all hold', () => {
    const deck = card({ kind: 'slides', updatedAt: NOW - 1000, system: { id: 'kit:x', label: 'X' } });
    expect(matchesFilters(deck, { ...NO_FILTERS, kind: 'slides', recent: true, system: 'kit:x' }, NOW)).toBe(
      true,
    );
    expect(matchesFilters(deck, { ...NO_FILTERS, kind: 'video' }, NOW)).toBe(false);
    expect(matchesFilters(deck, { ...NO_FILTERS, system: 'kit:y' }, NOW)).toBe(false);
    expect(
      matchesFilters(card({ updatedAt: NOW - RECENT_MS - 1 }), { ...NO_FILTERS, recent: true }, NOW),
    ).toBe(false);
  });

  it('a document of an app type is no design of any kind', () => {
    expect(matchesFilters(card({ appKind: 'Table' }), { ...NO_FILTERS, kind: 'site' }, NOW)).toBe(false);
  });

  it('the sort alone narrows nothing', () => {
    expect(isNarrowed({ ...NO_FILTERS, sort: SORT.name })).toBe(false);
    expect(isNarrowed({ ...NO_FILTERS, recent: true })).toBe(true);
  });
});

describe('sortCards', () => {
  const list = () => [
    card({ id: 'b', title: 'Beta', updatedAt: 2 }),
    card({ id: 'a', title: 'alpha', updatedAt: 1 }),
    card({ id: 'p', title: 'Zed', updatedAt: 0, pinned: true }),
  ];
  it('keeps pinned first under every sort', () => {
    expect(sortCards(list(), SORT.changed).map((c) => c.id)).toEqual(['p', 'b', 'a']);
    expect(sortCards(list(), SORT.oldest).map((c) => c.id)).toEqual(['p', 'a', 'b']);
    expect(sortCards(list(), SORT.name).map((c) => c.id)).toEqual(['p', 'a', 'b']);
  });
});

describe('folders', () => {
  const folders = [
    { id: 'f-1', name: 'Work', parent: '' },
    { id: 'f-2', name: 'Archive', parent: 'f-1' },
    { id: 'f-3', name: 'Home', parent: '' },
  ];
  it('lists children by name and counts a folder with what is under it', () => {
    expect(childrenOf(folders, '').map((f) => f.id)).toEqual(['f-3', 'f-1']);
    expect(
      projectsIn([card({ id: 'x', folder: 'f-2' }), card({ id: 'y', folder: 'f-3' })], folders, 'f-1'),
    ).toBe(1);
  });
  it('orders the choices: a top folder, then its own', () => {
    expect(folderChoices(folders).map((c) => `${c.depth}${c.folder.id}`)).toEqual(['0f-3', '0f-1', '1f-2']);
  });
  it('names the systems of the cards once', () => {
    const s = { id: 'k', label: 'K' };
    expect(systemsOf([card({ system: s }), card({ id: 'b', system: s }), card({ id: 'c' })])).toEqual([s]);
  });
});

describe('the way to a card (the tour)', () => {
  const folders = [
    { id: 'f-demo', name: 'Демо', parent: '' },
    { id: 'f-work', name: 'Работа', parent: '' },
    { id: 'f-sub', name: 'Архив', parent: 'f-work' },
  ];
  const view = { folders, here: '', trashOpen: false, matches: true, searching: false, organized: true };

  it('is the card where the card lies', () => {
    expect(routeToCard(card({ folder: null }), view)).toEqual({ route: ROUTE.card });
    expect(routeToCard(card({ folder: 'f-demo' }), { ...view, here: 'f-demo' })).toEqual({
      route: ROUTE.card,
    });
  });

  it('opens the folder the card lies in, one click at a time', () => {
    expect(routeToCard(card({ folder: 'f-demo' }), view)).toEqual({ route: ROUTE.folder, folder: 'f-demo' });
    expect(routeToCard(card({ folder: 'f-sub' }), view)).toEqual({ route: ROUTE.folder, folder: 'f-work' });
    expect(routeToCard(card({ folder: 'f-sub' }), { ...view, here: 'f-work' })).toEqual({
      route: ROUTE.folder,
      folder: 'f-sub',
    });
  });

  it('goes up from another folder', () => {
    expect(routeToCard(card({ folder: 'f-demo' }), { ...view, here: 'f-work' })).toEqual({ route: ROUTE.up });
    expect(routeToCard(card({ folder: null }), { ...view, here: 'f-demo' })).toEqual({ route: ROUTE.up });
  });

  it('asks to clear what hides the card, and leaves a search alone when the card shows in it', () => {
    expect(routeToCard(card({ folder: 'f-demo' }), { ...view, matches: false })).toEqual({
      route: ROUTE.reset,
    });
    expect(routeToCard(card({ folder: 'f-demo' }), { ...view, trashOpen: true })).toEqual({
      route: ROUTE.reset,
    });
    expect(routeToCard(card({ folder: 'f-demo' }), { ...view, searching: true })).toEqual({
      route: ROUTE.card,
    });
  });

  it('knows a card that is gone, and a library that is not laid out', () => {
    expect(routeToCard(undefined, view)).toEqual({ route: ROUTE.gone });
    expect(routeToCard(card({ folder: 'f-demo' }), { ...view, organized: false })).toEqual({
      route: ROUTE.card,
    });
  });
});
