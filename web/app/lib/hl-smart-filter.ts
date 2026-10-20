import { defaultFilter } from 'cmdk';

/**
 * Search for the palettes: every word of the query in any order, the Cyrillic e for yo, and the same keys typed in
 * the other keyboard layout (typing on the wrong one still finds the word). The layout tables below are data. cmdk's fuzzy score stays as the last resort.
 */

const EN = "qwertyuiop[]asdfghjkl;'zxcvbnm,.`";
const RU = 'йцукенгшщзхъфывапролджэячсмитьбюё';
const EN_TO_RU = new Map([...EN].map((char, i) => [char, RU[i]]));
const RU_TO_EN = new Map([...RU].map((char, i) => [char, EN[i]]));

const normalize = (text: string) => text.toLowerCase().replaceAll('ё', 'е');

const swapLayout = (text: string, table: Map<string, string>) =>
  [...text].map((char) => table.get(char) ?? char).join('');

/** The query as typed and as it would read in the other layout. */
export const queryVariants = (search: string): string[] => {
  const query = search.toLowerCase().trim();
  if (!query) return [];
  return [...new Set([query, swapLayout(query, EN_TO_RU), swapLayout(query, RU_TO_EN)].map(normalize))];
};

const wordScore = (haystack: string, query: string): number => {
  const words = query.split(/\s+/).filter(Boolean);
  if (!words.length) return 0;
  let first = Infinity;
  for (const word of words) {
    const at = haystack.indexOf(word);
    if (at < 0) return 0;
    first = Math.min(first, at);
  }
  // Earlier hits rank higher; a word at a word boundary a little higher still.
  const boundary = first === 0 || /[\s/.:_-]/.test(haystack[first - 1]) ? 0.05 : 0;
  return 0.8 + boundary + 0.15 / (1 + first / 40);
};

export const smartFilter = (value: string, search: string, keywords?: string[]): number => {
  const variants = queryVariants(search);
  if (!variants.length) return 1;
  const haystack = normalize([value, ...(keywords ?? [])].join(' '));
  let best = 0;
  for (const query of variants) {
    best = Math.max(best, wordScore(haystack, query));
    if (best > 0) continue;
    best = Math.max(best, defaultFilter(value, query, keywords) * 0.6);
  }
  return best;
};
