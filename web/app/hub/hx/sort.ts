// Sorting and searching of a table. The state lives in the address (`?sort=calls&dir=desc&q=text`), so a link opens the
// same view; changing it replaces the history entry, Back does not step through every click.
import { useCallback, useMemo } from 'react';

import { useLocation } from '../lib/router';

export type Dir = 'asc' | 'desc';

export interface SortState {
  key: string | null;
  dir: Dir;
}

export const readSort = (search: string, keys: readonly string[], fallback: SortState): SortState => {
  const q = new URLSearchParams(search);
  const key = q.get('sort');
  if (!key || !keys.includes(key)) return fallback;
  return { key, dir: q.get('dir') === 'asc' ? 'asc' : 'desc' };
};

/** The next state after a click on a header: a new column starts as `first`, the same one turns around. */
export const nextSort = (s: SortState, key: string, first: Dir = 'desc'): SortState =>
  s.key === key ? { key, dir: s.dir === 'desc' ? 'asc' : 'desc' } : { key, dir: first };

export type Getters<T> = Record<string, (row: T) => number | string>;

/** A stable sort by the chosen column; texts compare the Russian way, numbers as numbers. */
export const sortRows = <T>(rows: readonly T[], s: SortState, get: Getters<T>): T[] => {
  const g = s.key ? get[s.key] : undefined;
  if (!g) return [...rows];
  const sign = s.dir === 'asc' ? 1 : -1;
  return rows
    .map((row, i) => ({ row, i, v: g(row) }))
    .sort((a, b) => {
      const c =
        typeof a.v === 'number' && typeof b.v === 'number'
          ? a.v - b.v
          : String(a.v).localeCompare(String(b.v), 'ru');
      return c * sign || a.i - b.i;
    })
    .map((x) => x.row);
};

/** Rows that have the text in any of the fields `of` returns, ignoring case. */
export const searchRows = <T>(rows: readonly T[], text: string, of: (row: T) => string[]): T[] => {
  const q = text.trim().toLowerCase();
  return q ? rows.filter((r) => of(r).some((f) => f.toLowerCase().includes(q))) : [...rows];
};

/** The query string with some keys set (`null` removes one); the other keys stay. */
export const withParams = (search: string, next: Record<string, string | null>): string => {
  const q = new URLSearchParams(search);
  for (const [k, v] of Object.entries(next)) {
    if (v === null || v === '') q.delete(k);
    else q.set(k, v);
  }
  const s = q.toString();
  return s ? `?${s}` : '';
};

/** The address's parameters as state: `get` reads one, `set` changes several at once without a new history entry. */
export const useParams = () => {
  const { search } = useLocation();
  const q = useMemo(() => new URLSearchParams(search), [search]);
  const get = useCallback((k: string) => q.get(k), [q]);
  const set = useCallback((next: Record<string, string | null>) => {
    history.replaceState(history.state, '', `${location.pathname}${withParams(location.search, next)}`);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }, []);
  return { search, get, set };
};
