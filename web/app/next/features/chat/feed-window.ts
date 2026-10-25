// Which turns of a long feed are drawn (spec §6): the feed is windowed by turns, not entries; below 300 turns all of
// them are drawn (a folded turn is light), above it the turns around the viewport ±40, with spacers of the measured (or
// estimated) heights above and below, so the scrollbar and the position stay true.

export const VIRTUAL_FROM = 300;
export const OVERSCAN = 40;
/** The drawn range moves only when the viewport comes this close to its edge, so scrolling re-renders every ~20 turns
 *  instead of every turn. */
export const RESERVE = 20;
/** A folded turn with a short answer, before it is measured. */
export const ESTIMATED_TURN_PX = 160;

export interface IFeedWindow {
  start: number;
  end: number; // exclusive
  top: number; // px above the first drawn turn
  bottom: number; // px below the last drawn turn
  first: number; // the first turn in the viewport
  last: number; // the last turn in the viewport
}

export const FULL_WINDOW = (count: number): IFeedWindow => ({
  start: 0,
  end: count,
  top: 0,
  bottom: 0,
  first: 0,
  last: Math.max(0, count - 1),
});

const heightOf = (heights: ReadonlyMap<string, number>, key: string): number =>
  heights.get(key) ?? ESTIMATED_TURN_PX;

/** The window for a scroll position; `offset` is where the list starts inside the scroller (its padding). */
export const feedWindow = (
  keys: readonly string[],
  heights: ReadonlyMap<string, number>,
  scrollTop: number,
  viewport: number,
  offset = 0,
): IFeedWindow => {
  const count = keys.length;
  if (count < VIRTUAL_FROM) return FULL_WINDOW(count);
  const from = Math.max(0, scrollTop - offset);
  const to = from + viewport;
  let y = 0;
  let first = -1;
  let last = count - 1;
  for (let index = 0; index < count; index += 1) {
    const h = heightOf(heights, keys[index]);
    if (first < 0 && y + h > from) first = index;
    if (y >= to) {
      last = index - 1;
      break;
    }
    y += h;
  }
  if (first < 0) first = count - 1;
  const start = Math.max(0, first - OVERSCAN);
  const end = Math.min(count, Math.max(last, first) + OVERSCAN + 1);
  let top = 0;
  for (let index = 0; index < start; index += 1) top += heightOf(heights, keys[index]);
  let bottom = 0;
  for (let index = end; index < count; index += 1) bottom += heightOf(heights, keys[index]);
  return { start, end, top, bottom, first, last: Math.max(last, first) };
};

/** The drawn range [start, end) still has RESERVE turns on each side of the viewport (or reaches the list's edge). */
export const coversWindow = (start: number, end: number, next: IFeedWindow, count: number): boolean =>
  (start === 0 || next.first - start >= RESERVE) && (end === count || end - 1 - next.last >= RESERVE);

/** A new window is worth a render only when the drawn range changes. */
export const sameWindow = (a: IFeedWindow, b: IFeedWindow): boolean =>
  a.start === b.start && a.end === b.end && Math.abs(a.top - b.top) < 1 && Math.abs(a.bottom - b.bottom) < 1;

/** The window that keeps the newest turns drawn: where a feed opens (at its bottom). */
export const bottomWindow = (keys: readonly string[], heights: ReadonlyMap<string, number>): IFeedWindow => {
  const count = keys.length;
  if (count < VIRTUAL_FROM) return FULL_WINDOW(count);
  const start = Math.max(0, count - OVERSCAN * 2);
  let top = 0;
  for (let index = 0; index < start; index += 1) top += heightOf(heights, keys[index]);
  return { start, end: count, top, bottom: 0, first: count - 1, last: count - 1 };
};
