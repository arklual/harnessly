// How the slash palette finds a row: the query's letters in the name in their order (a fuzzy match), a run of them
// together and a start of a word scoring higher; the matched letters come back as ranges so the row can mark them.

/** [from, to) in the text. */
export type TRange = readonly [number, number];

export interface IMatch {
  /** Higher is closer; only for ordering the rows of one query. */
  score: number;
  ranges: TRange[];
}

const BOUNDARY = /[-_:./\s$@]/;

const atBoundary = (text: string, index: number): boolean => index === 0 || BOUNDARY.test(text[index - 1]);

/** The indexes joined into ranges: 2, 3, 4, 7 are [2, 5) and [7, 8). */
const joined = (indexes: readonly number[]): TRange[] => {
  const out: [number, number][] = [];
  for (const index of indexes) {
    const last = out[out.length - 1];
    if (last && last[1] === index) last[1] = index + 1;
    else out.push([index, index + 1]);
  }
  return out;
};

/** The text where the query is whole: the earliest place, one that starts a word first. */
const substringAt = (text: string, query: string): number => {
  let first = -1;
  for (let at = text.indexOf(query); at >= 0; at = text.indexOf(query, at + 1)) {
    if (first < 0) first = at;
    if (atBoundary(text, at)) return at;
  }
  return first;
};

/** The query's letters in the text in order, each as early as it can be; null where one is missing. */
const subsequence = (text: string, query: string): number[] | null => {
  const found: number[] = [];
  let from = 0;
  for (const letter of query) {
    // A letter that starts a word is taken over an earlier one inside a word, when the word is near.
    let at = text.indexOf(letter, from);
    if (at < 0) return null;
    if (!atBoundary(text, at)) {
      for (
        let next = text.indexOf(letter, at + 1);
        next >= 0 && next - at <= 3;
        next = text.indexOf(letter, next + 1)
      ) {
        if (atBoundary(text, next)) {
          at = next;
          break;
        }
      }
    }
    found.push(at);
    from = at + 1;
  }
  return found;
};

const SUBSTRING_BASE = 1000;
const SUBSEQUENCE_BASE = 400;
const PREFIX_BONUS = 300;
const WORD_BONUS = 150;

/** How `query` matches `text`, case aside; null: it does not. An empty query matches everything and marks nothing. */
export const fuzzyMatch = (query: string, text: string): IMatch | null => {
  const needle = query.toLowerCase();
  if (!needle) return { score: 0, ranges: [] };
  const hay = text.toLowerCase();
  const at = substringAt(hay, needle);
  if (at >= 0) {
    let score = SUBSTRING_BASE - at * 8 - (hay.length - needle.length);
    if (at === 0) score += PREFIX_BONUS;
    else if (atBoundary(hay, at)) score += WORD_BONUS;
    return { score, ranges: [[at, at + needle.length]] };
  }
  const indexes = subsequence(hay, needle);
  if (!indexes) return null;
  let score = SUBSEQUENCE_BASE - (indexes[indexes.length - 1] - indexes[0]);
  for (let i = 0; i < indexes.length; i += 1) {
    if (atBoundary(hay, indexes[i])) score += 30;
    if (i > 0 && indexes[i] === indexes[i - 1] + 1) score += 20;
  }
  return { score, ranges: joined(indexes) };
};

/** The best of several spellings of one name (its aliases); the ranges are of the one that won. */
export const bestMatch = (query: string, names: readonly string[]): (IMatch & { name: string }) | null => {
  let best: (IMatch & { name: string }) | null = null;
  for (const name of names) {
    const match = fuzzyMatch(query, name);
    if (match && (!best || match.score > best.score)) best = { ...match, name };
  }
  return best;
};

/** The text cut into parts, the matched ones marked: what a row draws with <mark>. */
export const highlighted = (text: string, ranges: readonly TRange[]): { text: string; hit: boolean }[] => {
  const parts: { text: string; hit: boolean }[] = [];
  let cursor = 0;
  for (const [from, to] of ranges) {
    if (from > cursor) parts.push({ text: text.slice(cursor, from), hit: false });
    parts.push({ text: text.slice(from, to), hit: true });
    cursor = to;
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor), hit: false });
  return parts;
};
