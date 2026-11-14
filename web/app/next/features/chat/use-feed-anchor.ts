import { useLayoutEffect, useRef, type RefObject } from 'react';

/** How far under the feed's top edge the reader's place is taken. */
const PROBE_PX = 24;
/** The width of the feed changed by at least this much: the lines are broken anew. */
const WIDTH_STEP_PX = 1;

interface IHold {
  el: Element;
  /** Its top below the feed's top edge, as the reader last saw it. */
  offset: number;
}

/**
 * The reader's place: the block at the top edge and the boxes it is in, up to its row of the feed. When the block is
 * drawn anew (a markdown that was plain text, a turn that folded its work) the nearest box still there holds the place.
 */
type IAnchor = IHold[];

/** The place of a block: it and the boxes around it up to its row, each with its top under the feed's top edge. */
const holdOf = (scroller: HTMLElement, block: Element): IAnchor => {
  const top = scroller.getBoundingClientRect().top;
  const chain: IAnchor = [];
  for (let el: Element | null = block; el && el !== scroller; el = el.parentElement) {
    chain.push({ el, offset: el.getBoundingClientRect().top - top });
    if (el.classList.contains('hx-turnwrap')) break;
  }
  return chain;
};

/** The block the reader is looking at: the box at the feed's top edge, up out of the line it is a part of. */
const blockAt = (scroller: HTMLElement): IAnchor | null => {
  const box = scroller.getBoundingClientRect();
  const inner = scroller.firstElementChild;
  const innerBox = inner?.getBoundingClientRect();
  const x = innerBox ? innerBox.left + innerBox.width / 2 : box.left + box.width / 2;
  let el = document.elementFromPoint?.(x, box.top + PROBE_PX) ?? null;
  while (el && el !== scroller && getComputedStyle(el).display.startsWith('inline')) el = el.parentElement;
  if (!el || el === scroller || el === inner || !scroller.contains(el)) return null;
  // The probe fell into a box that holds several (a gap between the parts of a turn): the place is the first of its
  // boxes still on screen, as the browser's own scroll anchoring takes it, so it does not move when the part above it
  // folds.
  let block: Element = el;
  for (;;) {
    const child: Element | undefined = [...block.children].find((part: Element) => {
      const r = part.getBoundingClientRect();
      return r.height > 0 && r.bottom > box.top && !getComputedStyle(part).display.startsWith('inline');
    });
    if (!child) break;
    block = child;
  }
  return holdOf(scroller, block);
};

/** How far the noted block moved from where the reader saw it; 0 when nothing of it is left to measure. */
export const anchorShift = (scroller: HTMLElement, noted: IAnchor | null): number => {
  const hold = noted?.find((part) => part.el.isConnected);
  if (!hold) return 0;
  return hold.el.getBoundingClientRect().top - scroller.getBoundingClientRect().top - hold.offset;
};

/** Moves the feed so the noted block stands where the reader saw it; returns the scroll it made. */
const scrollBack = (scroller: HTMLElement, noted: IAnchor | null): number => {
  const shift = anchorShift(scroller, noted);
  if (Math.abs(shift) < 1) return 0;
  const before = scroller.scrollTop;
  scroller.scrollTop = before + shift;
  return scroller.scrollTop - before;
};

export interface IFeedAnchor {
  /** Notes the block at the top edge after this frame's layout (after the turns are measured). */
  note: () => void;
  /** Notes it now: the reader scrolled, and what changes next must keep this place. */
  noteNow: () => void;
  /**
   * Away from the bottom, the block the reader had at the top edge goes back to it: whatever grew or shrank above it
   * (a picture or a code block came, a turn folded its work, a card came in) does not move what they read. The feed's
   * own scroll anchoring is off (Safari has none, and it fights the bottom): this is the one for every browser. Returns
   * the scroll the feed made.
   */
  keep: () => number;
  /** Notes a given block as the place: the answer the feed stood at, kept whole while its markdown is drawn anew. */
  noteAt: (block: Element) => void;
}

/**
 * Keeps the reader's place when the feed's width or its rows' heights change. At the bottom the feed stays at the
 * bottom (the feed itself follows there); elsewhere the block that was at the top edge stays at it.
 */
const useFeedAnchor = (
  scroller: RefObject<HTMLElement | null>,
  atBottom: RefObject<boolean>,
): IFeedAnchor => {
  const anchor = useRef<IAnchor | null>(null);
  const frame = useRef(0);

  const noteNow = () => {
    cancelAnimationFrame(frame.current);
    const el = scroller.current;
    if (el && !atBottom.current) anchor.current = blockAt(el);
  };

  const note = () => {
    cancelAnimationFrame(frame.current);
    frame.current = requestAnimationFrame(noteNow);
  };

  /** Notes this block as the reader's place, where it stands now (the feed stood at its start). */
  const noteAt = (block: Element) => {
    cancelAnimationFrame(frame.current);
    const el = scroller.current;
    if (el) anchor.current = holdOf(el, block);
  };

  const keep = (): number => {
    const el = scroller.current;
    return el && !atBottom.current ? scrollBack(el, anchor.current) : 0;
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
      scrollBack(el, anchor.current);
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
      cancelAnimationFrame(frame.current);
    };
  }, [scroller, atBottom]);

  return { note, noteNow, noteAt, keep };
};

export default useFeedAnchor;
