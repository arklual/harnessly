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
