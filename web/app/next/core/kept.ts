// A list rebuilt from the stores keeps the items that did not change: a row memoized on its item draws only
// when its own item changed, and a list whose every item stayed is the same list.

import { useState } from 'react';

/** Equal when every field is the same value, or holds the same data (an object of a field compared by its JSON). */
export const sameFields = <T extends object>(a: T, b: T): boolean => {
  if (a === b) return true;
  const ka = Object.keys(a) as (keyof T)[];
  if (ka.length !== Object.keys(b).length) return false;
  return ka.every((k) => {
    const x = a[k];
    const y = b[k];
    if (Object.is(x, y)) return true;
    return (
      typeof x === 'object' &&
      typeof y === 'object' &&
      x !== null &&
      y !== null &&
      JSON.stringify(x) === JSON.stringify(y)
    );
  });
};

/** The items of `next` with each one equal to the item of `was` under its key kept as it was; `was` itself when all stayed. */
export const keepItems = <T extends object>(
  was: readonly T[] | null,
  next: readonly T[] | null,
  keyOf: (item: T) => string,
  same: (a: T, b: T) => boolean = sameFields,
): readonly T[] | null => {
  if (was === null || next === null || was === next) return next;
  const byKey = new Map(was.map((item) => [keyOf(item), item] as const));
  const out = next.map((item) => {
    const old = byKey.get(keyOf(item));
    return old !== undefined && same(old, item) ? old : item;
  });
  return out.length === was.length && out.every((item, i) => item === was[i]) ? was : out;
};

/** keepItems across renders: the list the component got last, its unchanged items kept. */
export function useKeptItems<T extends object>(
  next: readonly T[],
  keyOf: (item: T) => string,
  same?: (a: T, b: T) => boolean,
): readonly T[];
export function useKeptItems<T extends object>(
  next: readonly T[] | null,
  keyOf: (item: T) => string,
  same?: (a: T, b: T) => boolean,
): readonly T[] | null;
export function useKeptItems<T extends object>(
  next: readonly T[] | null,
  keyOf: (item: T) => string,
  same: (a: T, b: T) => boolean = sameFields,
): readonly T[] | null {
  const [kept, setKept] = useState({ from: next, out: next });
  if (kept.from === next) return kept.out;
  const out = keepItems(kept.out, next, keyOf, same);
  setKept({ from: next, out });
  return out;
}
