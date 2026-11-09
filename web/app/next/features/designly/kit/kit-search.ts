// The kit's search (⌘K): the canvas's index from the runtime filtered by the same rule an agent's search uses
// (internal/designkit/index.go, both checked against internal/designkit/testdata/search-cases.json): terms cut to their
// stems, an entry found when it answers at least half of them, more answered terms first. Results come grouped by kind.
import type { IIndexEntry } from '@/next/features/designly/kit/kit-api';

export const RESULT_KINDS = ['component', 'layout', 'template', 'asset', 'color', 'type', 'token'] as const;
export type TResultKind = (typeof RESULT_KINDS)[number];

export const queryTerms = (q: string): string[] =>
  q
    .toLowerCase()
    .replaceAll('ё', 'е')
    .split(/[\s,]+/)
    .map((t) => t.replace(/^#/, ''))
    .filter((t) => t !== '');

/** Endings cut off a term, longest first (index.go's stemEndings); a stem keeps at least STEM_MIN letters. */
// prettier-ignore
const STEM_ENDINGS = [
  'ированием', 'ирование', 'ирования', 'ованием', 'ование', 'ования', 'ениями', 'ениям', 'ением', 'ениях',
  'ение', 'ения', 'ений', 'ению', 'ании', 'ание', 'ания', 'аниям', 'ться', 'иями', 'ями', 'ами', 'ого', 'его',
  'ому', 'ему', 'ыми', 'ими', 'ией', 'ость', 'ости', 'ая', 'яя', 'ое', 'ее', 'ые', 'ие', 'ый', 'ий', 'ой', 'ую',
  'юю', 'ов', 'ев', 'ах', 'ях', 'ом', 'ем', 'ам', 'ям', 'ть', 'ет', 'ит', 'ут', 'ют', 'ат', 'ят', 'а', 'я', 'о',
  'е', 'ы', 'и', 'у', 'ю', 'ь', 'й',
  'ations', 'ation', 'ings', 'ing', 'ers', 'er', 'ies', 'ied', 'ed', 'es', 's',
] as const;
const STEM_MIN = 3;

/** A term without its ending; a short term, a number or a colour stays whole. */
export const stem = (term: string): string => {
  const r = [...term];
  if (r.length <= STEM_MIN + 1) return term;
  for (const e of STEM_ENDINGS) {
    const n = [...e].length;
    if (r.length - n >= STEM_MIN && term.endsWith(e)) return r.slice(0, r.length - n).join('');
  }
  return term;
};

/** How well an entry answers the terms, 0 when it answers fewer than half of them. */
const scoreOf = (e: IIndexEntry, terms: readonly string[]): number => {
  const title = e.title.toLowerCase().replaceAll('ё', 'е');
  const text = e.words.replaceAll('ё', 'е');
  let hit = 0;
  for (const t of terms) {
    const s = stem(t);
    if (text.includes(s) || title.includes(s)) hit += 1;
  }
  if (hit === 0 || hit * 2 < terms.length) return 0;
  let s = hit * 10;
  const first = stem(terms[0] ?? '');
  if (title === terms[0]) s += 4;
  else if (title.startsWith(first)) s += 3;
  else if (title.includes(first)) s += 2;
  return s;
};

/** The entries that answer the query, best first, at most `limit` (all when it is 0). */
export const searchIndex = (index: readonly IIndexEntry[], q: string, limit = 60): IIndexEntry[] => {
  const terms = queryTerms(q);
  if (terms.length === 0) return [];
  return index
    .map((e, i) => ({ e, s: scoreOf(e, terms), i }))
    .filter((h) => h.s > 0)
    .sort((a, b) => b.s - a.s || a.i - b.i)
    .slice(0, limit > 0 ? limit : undefined)
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
