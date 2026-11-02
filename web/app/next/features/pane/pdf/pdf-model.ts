// The PDF view's arithmetic. Pure: the view and its tests read it. Pages stand one under another, each its proportions at
// one width, so where every page is follows from the proportions alone and no page has to be drawn to be scrolled to.

export const PAGE_GAP = 12;
export const ZOOM_MIN = 0.5;
export const ZOOM_MAX = 4;
const ZOOM_STEP = 1.25;
/** Pages drawn on each side of the one in view; the others hold no canvas, so a long file takes no more memory. */
export const NEAR_PAGES = 3;

/** How far from the top each page starts, and the whole height as one more value at the end. */
export const offsetsOf = (heights: readonly number[], gap: number = PAGE_GAP): number[] => {
  const offsets = [0];
  for (const height of heights) offsets.push(offsets[offsets.length - 1] + height + gap);
  return offsets;
};

/** The index of the page under this scroll position. */
export const pageAt = (offsets: readonly number[], top: number): number => {
  let low = 0;
  let high = Math.max(0, offsets.length - 2);
  while (low < high) {
    const middle = Math.ceil((low + high) / 2);
    if (offsets[middle] <= top) low = middle;
    else high = middle - 1;
  }
  return low;
};

export const zoomIn = (zoom: number): number => Math.min(ZOOM_MAX, zoom * ZOOM_STEP);
export const zoomOut = (zoom: number): number => Math.max(ZOOM_MIN, zoom / ZOOM_STEP);

/** The page number typed in the field as an index; null when it is no page of the file. */
export const typedPage = (typed: string, count: number): number | null => {
  const page = Number(typed.trim());
  return Number.isInteger(page) && page >= 1 && page <= count ? page - 1 : null;
};

const fold = (text: string) => text.toLocaleLowerCase().replaceAll('ё', 'е');

/** Where the query stands in a text, by character. Folding keeps the length, so these are places in the text itself. */
export const hitsIn = (text: string, query: string): number[] => {
  const needle = fold(query.trim());
  if (needle === '') return [];
  const hay = fold(text);
  const hits: number[] = [];
  let at = hay.indexOf(needle);
  while (at !== -1) {
    hits.push(at);
    at = hay.indexOf(needle, at + needle.length);
  }
  return hits;
};

export interface IPdfHit {
  page: number;
  /** Which match of the page. */
  nth: number;
}

/** Every match of the query, page by page. A page whose text is not read yet (null) has none so far. */
export const hitsOf = (pages: readonly (string | null)[], query: string): IPdfHit[] =>
  pages.flatMap((text, page) => (text === null ? [] : hitsIn(text, query).map((_, nth) => ({ page, nth }))));

/** The next match going `step` from `index`, round the ends. */
export const stepHit = (index: number, count: number, step: 1 | -1): number =>
  count === 0 ? 0 : (index + step + count) % count;
