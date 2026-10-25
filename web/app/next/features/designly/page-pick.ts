// The list of a design's pages in the pane's picker: the search by name or path, and the row the arrows stand on.
import type { ISitePage } from '@/next/features/designly/site-view';

/** More pages than this get a search field. */
export const SEARCH_FROM = 5;

/** The pages whose name or path holds what was typed (case aside); all of them for an empty query. */
export const pagesFound = (pages: readonly ISitePage[], query: string): ISitePage[] => {
  const words = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (words.length === 0) return [...pages];
  return pages.filter((p) => {
    const hay = `${p.name} ${p.path}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
};

/** The row after a key: ↑ and ↓ go round the list, Home and End to its ends; -1 where there is no row. */
export const rowAfter = (key: string, at: number, count: number): number => {
  if (count === 0) return -1;
  if (key === 'ArrowDown') return at < 0 ? 0 : (at + 1) % count;
  if (key === 'ArrowUp') return at <= 0 ? count - 1 : at - 1;
  if (key === 'Home') return 0;
  if (key === 'End') return count - 1;
  return at;
};
