type TOverflowOf = (el: Element) => string;

const SCROLLING = new Set(['auto', 'scroll', 'overlay']);

const computedOverflowY: TOverflowOf = (el) => getComputedStyle(el).overflowY;

export const canScrollY = (
  el: Element,
  deltaY: number,
  overflowOf: TOverflowOf = computedOverflowY,
): boolean => {
  if (!SCROLLING.has(overflowOf(el)) || el.scrollHeight <= el.clientHeight) return false;
  return deltaY < 0 ? el.scrollTop > 0 : el.scrollTop + el.clientHeight < el.scrollHeight - 1;
};

/** Whether a box between the pointer and `boundary` (exclusive) scrolls this wheel step itself. */
export const wheelHandledInside = (
  target: Element,
  boundary: Element,
  deltaY: number,
  overflowOf: TOverflowOf = computedOverflowY,
): boolean => {
  for (let el: Element | null = target; el && el !== boundary; el = el.parentElement) {
    if (canScrollY(el, deltaY, overflowOf)) return true;
  }
  return false;
};
