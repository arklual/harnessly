import { describe, expect, it } from 'vitest';
import { MAX_SEEN, MAX_UNSEEN, newsFor } from './news';
import { isSeen, parseSeen, SEEN_ALL, seenAll, type IEntry } from './seen';

const entry = (id: string, date: string): IEntry => ({ id, date, icon: 'pencil' });
const ENTRIES = [
  entry('old', '2026-10-27'),
  entry('a', '2026-11-02'),
  entry('c', '2026-11-03'),
  entry('b', '2026-11-02'),
  entry('d', '2026-11-03'),
];
const ids = (cards: { id: string; unseen: boolean }[] | null) =>
  cards?.map((card) => (card.unseen ? `${card.id}*` : card.id));

describe('newsFor', () => {
  it('puts the untold first, newest day first, then the told ones', () => {
    expect(ids(newsFor(ENTRIES, { upTo: '2026-11-02', ids: [] }))).toEqual(['c*', 'd*', 'a', 'b', 'old']);
    expect(ids(newsFor(ENTRIES, { upTo: '2026-10-27', ids: [] }))).toEqual(['c*', 'd*', 'a*', 'b*', 'old']);
  });

  it('tells of an entry merged late, though its day is older than the newest told', () => {
    const seen = { upTo: '2026-10-27', ids: ['a', 'c', 'd'] };
    expect(ids(newsFor(ENTRIES, seen))).toEqual(['b*', 'c', 'd', 'a', 'old']);
  });

  it('tells of the newest day alone to a browser that was told of nothing', () => {
    expect(ids(newsFor(ENTRIES, null))).toEqual(['c*', 'd*', 'a', 'b', 'old']);
  });

  it('stays quiet when everything is told', () => {
    expect(newsFor(ENTRIES, seenAll(ENTRIES))).toBeNull();
    expect(newsFor(ENTRIES, parseSeen(SEEN_ALL))).toBeNull();
    expect(newsFor([], null)).toBeNull();
  });

  it('keeps to what the strip has room for', () => {
    const day = (date: string) => Array.from({ length: 20 }, (_, i) => entry(`${date}-${i}`, date));
    const cards =
      newsFor([...day('2026-11-03'), ...day('2026-11-02')], { upTo: '2026-11-02', ids: [] }) ?? [];
    expect(cards.filter((card) => card.unseen)).toHaveLength(MAX_UNSEEN);
    expect(cards.filter((card) => !card.unseen)).toHaveLength(MAX_SEEN);
  });
});

describe('seen', () => {
  it('reads the release id kept before entries were files as a day', () => {
    expect(parseSeen('2026-11-11zd')).toEqual({ upTo: '2026-11-11', ids: [] });
    expect(parseSeen('2026-11-15')).toEqual({ upTo: '2026-11-15', ids: [] });
  });

  it('reads what it writes and drops what it cannot read', () => {
    const seen = seenAll(ENTRIES);
    expect(parseSeen(JSON.stringify(seen))).toEqual(seen);
    expect(parseSeen(null)).toBeNull();
    expect(parseSeen('{broken')).toBeNull();
    expect(parseSeen('garbage')).toBeNull();
  });

  it('keeps the last month by id and the rest by day', () => {
    const seen = seenAll([
      entry('now', '2026-11-21'),
      entry('month', '2026-10-25'),
      entry('long', '2026-09-01'),
    ]);
    expect(seen).toEqual({ upTo: '2026-10-22', ids: ['now', 'month'] });
    expect(isSeen(seen, entry('long', '2026-09-01'))).toBe(true);
    expect(isSeen(seen, entry('late', '2026-11-10'))).toBe(false);
  });
});
