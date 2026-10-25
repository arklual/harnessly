// A horizontal strip (the design's hsSync/hsStep): the edge with more behind it says so in data-hs-l / data-hs-r (the
// stylesheet fades that edge), the arrows step by 80% of the strip, and the current item is brought into view when it
// changes. Edges are measured once a frame, after a scroll, a resize or a change of the strip's content.
import { useCallback, useEffect, type RefObject } from 'react';

/** How far from an edge counts as at the edge, px. */
const EDGE_PX = 2;
/** Room kept around the current item when it is scrolled into view, px. */
const CURRENT_GAP_PX = 24;
/** One arrow step, as a share of the strip's width. */
const STEP_SHARE = 0.8;
/** What marks the current item of a strip: a tab or a selected option. */
const CURRENT_SELECTOR = '[aria-current="true"], [aria-selected="true"]';

export const HSCROLL_DIR = { prev: -1, next: 1 } as const;
export type THScrollDir = (typeof HSCROLL_DIR)[keyof typeof HSCROLL_DIR];

const calm = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

const flag = (el: HTMLElement, name: string, on: boolean) => {
  if (el.hasAttribute(name) !== on) el.toggleAttribute(name, on);
};

/** Marks the edges of the strip that have more behind them. */
export const syncEdges = (el: HTMLElement) => {
  flag(el, 'data-hs-l', el.scrollLeft > EDGE_PX);
  flag(el, 'data-hs-r', el.scrollLeft + el.clientWidth < el.scrollWidth - EDGE_PX);
};

/** Where the strip must scroll to show the item with its gap, or null when it is in view already. */
export const scrollTarget = (el: HTMLElement, item: HTMLElement): number | null => {
  const left = item.getBoundingClientRect().left - el.getBoundingClientRect().left + el.scrollLeft;
  const start = left - CURRENT_GAP_PX;
  const end = left + item.offsetWidth + CURRENT_GAP_PX - el.clientWidth;
  if (el.scrollLeft > start) return start;
  if (el.scrollLeft < end) return end;
  return null;
};

/** Keeps the edge marks and the current item of the strip in `ref`; returns the arrows' step. */
export const useHScroll = (ref: RefObject<HTMLElement | null>) => {
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let frame = 0;
    let followed: Element | null = null;
    const measure = () => {
      frame = 0;
      syncEdges(el);
      const item = el.querySelector<HTMLElement>(CURRENT_SELECTOR);
      if (!item || item === followed) return;
      followed = item;
      const left = scrollTarget(el, item);
      if (left !== null) el.scrollTo({ left, behavior: calm() ? 'auto' : 'smooth' });
    };
    const soon = () => {
      if (!frame) frame = requestAnimationFrame(measure);
    };
    el.addEventListener('scroll', soon, { passive: true });
    const resize = new ResizeObserver(soon);
    resize.observe(el);
    const mutate = new MutationObserver(soon);
    mutate.observe(el, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ['aria-current', 'aria-selected'],
    });
    soon();
    return () => {
      el.removeEventListener('scroll', soon);
      resize.disconnect();
      mutate.disconnect();
      cancelAnimationFrame(frame);
    };
  }, [ref]);

  return useCallback(
    (dir: THScrollDir) => {
      const el = ref.current;
      if (!el) return;
      el.scrollBy({
        left: dir * Math.round(el.clientWidth * STEP_SHARE),
        behavior: calm() ? 'auto' : 'smooth',
      });
    },
    [ref],
  );
};
