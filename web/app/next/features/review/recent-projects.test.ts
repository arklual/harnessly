import { describe, expect, it } from 'vitest';
import { RECENT_MAX, parseRecent, pushRecent, withRecentFirst } from '@/next/features/review/recent-projects';

describe('recent projects', () => {
  it('reads only what it understands', () => {
    expect(parseRecent(null)).toEqual([]);
    expect(parseRecent('{')).toEqual([]);
    expect(parseRecent('[{"path":"a/b","name":"b"},{"path":""},{"x":1}]')).toEqual([
      { path: 'a/b', name: 'b' },
    ]);
  });

  it('puts the opened one on top, once, and keeps a few', () => {
    let list = pushRecent([], { path: 'a/x', name: 'x' });
    list = pushRecent(list, { path: 'a/y', name: 'y' });
    list = pushRecent(list, { path: 'a/x', name: 'x' });
    expect(list.map((x) => x.path)).toEqual(['a/x', 'a/y']);
    for (let i = 0; i < RECENT_MAX + 3; i++) list = pushRecent(list, { path: `p/${i}`, name: `${i}` });
    expect(list).toHaveLength(RECENT_MAX);
  });

  it('lists the found recent ones first, in their order', () => {
    const found = [{ path: 'a' }, { path: 'b' }, { path: 'c' }, { path: 'd' }];
    const recent = [
      { path: 'c', name: 'c' },
      { path: 'gone', name: 'gone' },
      { path: 'b', name: 'b' },
    ];
    expect(withRecentFirst(found, recent).map((x) => x.path)).toEqual(['c', 'b', 'a', 'd']);
  });
});
