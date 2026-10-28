import { describe, expect, it } from 'vitest';
import { mergeComments, pinsOf, unresolvedCount } from '@/next/features/designly/deck/comments-model';
import type { IDeckComment, IDeckSlide } from '@/next/features/designly/deck/types';

const slide = (id: string): IDeckSlide => ({
  id,
  path: `${id}.dc.html`,
  title: id,
  w: 1920,
  h: 1080,
  notes: '',
  voice: false,
  seconds: 10,
  version: 'v1',
  blank: false,
});
const team = (id: string, page: string, at: string, resolved = false): IDeckComment => ({
  id,
  page,
  x: 10,
  y: 20,
  label: 'Схема',
  text: id,
  by: { name: 'Рома', email: 'roma@example.org' },
  at,
  resolved,
  resolvedBy: null,
  resolvedAt: null,
});
const guest = (id: number, board: string, at: string) => ({
  id,
  linkId: 'l',
  linkTitle: '',
  board,
  x: 0.5,
  y: null,
  text: `g${id}`,
  name: id === 1 ? 'Анна' : '',
  at,
  label: '',
});

describe('mergeComments', () => {
  const slides = [slide('a'), slide('b')];
  it('merges team and guests by time and numbers them', () => {
    const list = mergeComments(
      slides,
      [team('t1', 'b', '2026-10-27T10:00:02Z')],
      [guest(1, 'a.dc.html', '2026-10-27T10:00:01Z'), guest(2, 'gone.dc.html', '2026-10-27T10:00:03Z')],
      [],
    );
    expect(list.map((c) => [c.key, c.n, c.slide])).toEqual([
      ['g-1', 1, 0],
      ['t1', 2, 1],
    ]);
    expect(list[0]).toMatchObject({ guest: true, who: 'Анна', x: 50, y: null });
  });
  it('marks resolved guests and counts the unresolved', () => {
    const list = mergeComments(
      slides,
      [team('t1', 'a', '2026-10-27T10:00:00Z', true)],
      [guest(1, 'a.dc.html', '2026-10-27T10:00:01Z')],
      ['g-1'],
    );
    expect(unresolvedCount(list)).toBe(0);
    expect(pinsOf(list, 0)).toEqual([]);
  });
  it('pins are unresolved comments with a point on the slide', () => {
    const list = mergeComments(
      slides,
      [team('t1', 'a', '2026-10-27T10:00:00Z')],
      [guest(2, 'a.dc.html', '2026-10-27T10:00:01Z')],
      [],
    );
    expect(pinsOf(list, 0).map((c) => c.key)).toEqual(['t1']);
    expect(unresolvedCount(list)).toBe(2);
  });
  it('a deck not read has no guests: team only', () => {
    expect(mergeComments(slides, [team('t1', 'a', 'x')], null, [])).toHaveLength(1);
  });
});
