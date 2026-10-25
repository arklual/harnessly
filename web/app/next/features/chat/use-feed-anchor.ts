import { useLayoutEffect, useRef, type RefObject } from 'react';

/** How far under the feed's top edge the reader's place is taken. */
const PROBE_PX = 24;
/** The width of the feed changed by at least this much: the lines are broken anew. */
const WIDTH_STEP_PX = 1;

interface IAnchor {
  el: Element;
  /** Its top below the feed's top edge, as the reader last saw it. */
  offset: number;
}

/** The block the reader is looking at: the box at the feed's top edge, up out of the line it is a part of. */
const blockAt = (scroller: HTMLElement): IAnchor | null => {
  const box = scroller.getBoundingClientRect();
  const inner = scroller.firstElementChild;
  const innerBox = inner?.getBoundingClientRect();
  const x = innerBox ? innerBox.left + innerBox.width / 2 : box.left + box.width / 2;
  let el = document.elementFromPoint(x, box.top + PROBE_PX);
  while (el && el !== scroller && getComputedStyle(el).display.startsWith('inline')) el = el.parentElement;
  if (!el || el === scroller || el === inner || !scroller.contains(el)) return null;
  return { el, offset: el.getBoundingClientRect().top - box.top };
};

/**
 * Keeps the reader's place when the feed's width changes (the right pane opens or closes, the sidebar folds): the lines
 * break anew, the turns grow or shrink, and a scroll position that stayed where it was would show another place. At the
 * bottom the feed stays at the bottom; elsewhere the block that was at the top edge stays at it. The place is noted
 * while the reader scrolls and after the turns are measured (`note`), so it is the one from before the width changed.
 */
const useFeedAnchor = (scroller: RefObject<HTMLElement | null>, atBottom: RefObject<boolean>) => {
  const anchor = useRef<IAnchor | null>(null);
  const frame = useRef(0);

  const note = () => {
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(() => {
      const el = scroller.current;
      if (el && !atBottom.current) anchor.current = blockAt(el);
    });
  };

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    let width = el.clientWidth;
    const observer = new ResizeObserver(() => {
      const next = el.clientWidth;
      if (Math.abs(next - width) < WIDTH_STEP_PX) return;
      width = next;
      if (atBottom.current) {
        el.scrollTop = el.scrollHeight;
        return;
      }
      const noted = anchor.current;
      if (!noted?.el.isConnected) return;
      const shift = noted.el.getBoundingClientRect().top - el.getBoundingClientRect().top - noted.offset;
      if (Math.abs(shift) >= 1) el.scrollTop += shift;
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame.current);
    };
  }, [scroller, atBottom]);

  return note;
};

export default useFeedAnchor;
