// The kit's search (⌘K): the canvas's index from the runtime filtered by the same rule an agent's search uses
// (internal/designkit/index.go): every term of the query in the entry's words, a title that starts with the first term
// first. Results come grouped by kind.
import type { IIndexEntry } from '@/next/features/designly/kit/kit-api';

export const RESULT_KINDS = ['component', 'layout', 'template', 'asset', 'color', 'type', 'token'] as const;
export type TResultKind = (typeof RESULT_KINDS)[number];

export const queryTerms = (q: string): string[] =>
  q
    .toLowerCase()
    .split(/[\s,]+/)
    .map((t) => t.replace(/^#/, ''))
    .filter((t) => t !== '');

const scoreOf = (e: IIndexEntry, terms: readonly string[]): number => {
  const title = e.title.toLowerCase();
  let s = 1;
  for (const [i, t] of terms.entries()) {
    if (!e.words.includes(t) && !title.includes(t)) return 0;
    if (i !== 0) continue;
    if (title === t) s += 4;
    else if (title.startsWith(t)) s += 3;
    else if (title.includes(t)) s += 2;
  }
  return s;
};

/** The entries that answer the query, best first, at most `limit`. */
export const searchIndex = (index: readonly IIndexEntry[], q: string, limit = 60): IIndexEntry[] => {
  const terms = queryTerms(q);
  if (terms.length === 0) return [];
  return index
    .map((e, i) => ({ e, s: scoreOf(e, terms), i }))
    .filter((h) => h.s > 0)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .slice(0, limit)
    .map((h) => h.e);
};

export interface IResultGroup {
  kind: TResultKind;
  entries: IIndexEntry[];
}

/** Results by kind, the kind of the best hit first (Enter takes the best hit), each group keeping the rank inside it. */
export const groupResults = (hits: readonly IIndexEntry[]): IResultGroup[] => {
  const known = new Set<string>(RESULT_KINDS);
  const order: TResultKind[] = [];
  for (const h of hits) {
    if (known.has(h.kind) && !order.includes(h.kind as TResultKind)) order.push(h.kind as TResultKind);
  }
  return order.map((kind) => ({ kind, entries: hits.filter((h) => h.kind === kind) }));
};
