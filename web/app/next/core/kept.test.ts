import { describe, expect, it } from 'vitest';
import { keepItems, sameFields } from '@/next/core/kept';

const id = (x: { id: string }) => x.id;

describe('keepItems (HRN-1449)', () => {
  it('keeps the items that did not change and takes the ones that did', () => {
    const was = [
      { id: 'a', n: 1, from: { app: 'x' } },
      { id: 'b', n: 1 },
    ];
    const next = [
      { id: 'a', n: 1, from: { app: 'x' } },
      { id: 'b', n: 2 },
    ];
    const out = keepItems(was, next, id);
    expect(out?.[0]).toBe(was[0]);
    expect(out?.[1]).toBe(next[1]);
  });

  it('gives the same list back when every item stayed', () => {
    const was = [{ id: 'a', n: 1 }];
    expect(keepItems(was, [{ id: 'a', n: 1 }], id)).toBe(was);
  });

  it('a new item, a gone one and a new order make a new list', () => {
    const was = [
      { id: 'a', n: 1 },
      { id: 'b', n: 1 },
    ];
    const out = keepItems(
      was,
      [
        { id: 'b', n: 1 },
        { id: 'a', n: 1 },
      ],
      id,
    );
    expect(out).not.toBe(was);
    expect(out?.[0]).toBe(was[1]);
  });

  it('sameFields tells a field that was dropped', () => {
    expect(sameFields<{ a: number; b?: number }>({ a: 1, b: 2 }, { a: 1 })).toBe(false);
  });
});
