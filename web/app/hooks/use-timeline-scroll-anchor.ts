import { useLayoutEffect, useRef, type RefObject } from 'react';
import {
  anchorCorrection,
  GESTURE_WINDOW_MS,
  heldHeight,
  HOLD_MS,
  isAtEnd,
  takeAnchor,
  type IEntryBox,
  type IScrollAnchor,
} from '@/lib/timeline-scroll-anchor';

const ENTRY_SELECTOR = '[data-entry-id]';
const GESTURES = ['wheel', 'touchstart', 'touchmove', 'pointerdown', 'keydown'] as const;

/** The first entry still visible at the top, found by halving: entries come in page order, so their tops grow. */
const entriesAround = (content: HTMLElement, scrollerTop: number): IEntryBox[] => {
  const nodes = content.querySelectorAll<HTMLElement>(ENTRY_SELECTOR);
  let low = 0;
  let high = nodes.length - 1;
  while (low < high) {
    const mid = (low + high) >> 1;
    if (nodes[mid].getBoundingClientRect().bottom > scrollerTop + 1) high = mid;
    else low = mid + 1;
  }
  const boxes: IEntryBox[] = [];
  for (let i = low; i < Math.min(nodes.length, low + 3); i++) {
    const rect = nodes[i].getBoundingClientRect();
    const id = nodes[i].dataset.entryId;
    if (id) boxes.push({ id, top: rect.top, bottom: rect.bottom });
  }
  return boxes;
};

/**
 * Keeps the reader where they are while the timeline changes under them: at the end stays at the end, an
 * entry being read stays at the same height on screen. Only the person moves the position — a scroll that comes with
 * a change of the content's height is the layout's, and the anchor taken before it is put back.
 */
export const useTimelineScrollAnchor = (
  scrollRef: RefObject<HTMLElement | null>,
  contentRef: RefObject<HTMLElement | null>,
  toEnd: () => void,
  sessionKey: string | null,
  version: unknown,
  /** False while the scroller is not on the page (a skeleton stands in); the hook attaches when it appears. */
  ready = true,
) => {
  const anchor = useRef<IScrollAnchor>({ atEnd: true, entryId: null, offset: 0 });
  const correctRef = useRef<() => void>(() => {});
  const toEndRef = useRef(toEnd);

  useLayoutEffect(() => {
    toEndRef.current = toEnd;
  }, [toEnd]);

  const held = useRef<number | null>(null);

  // Another conversation starts at its end, holding nothing.
  useLayoutEffect(() => {
    anchor.current = { atEnd: true, entryId: null, offset: 0 };
    held.current = null;
    if (contentRef.current) contentRef.current.style.minHeight = '';
  }, [sessionKey, contentRef]);

  useLayoutEffect(() => {
    const scroller = scrollRef.current;
    const content = contentRef.current;
    if (!scroller || !content) return;
    let gestureAt = 0;
    let lastHeight = content.offsetHeight;
    const recent = () => performance.now() - gestureAt < GESTURE_WINDOW_MS;
    const visible = () => scroller.clientHeight > 0;

    const rowsHeight = () => {
      const last = content.lastElementChild;
      return last ? last.getBoundingClientRect().bottom - content.getBoundingClientRect().top : 0;
    };
    let heldSince: number | null = null;
    let release: ReturnType<typeof setTimeout> | null = null;
    const hold = () => {
      const now = performance.now();
      const next = heldHeight(held.current, {
        box: content.offsetHeight,
        rows: rowsHeight(),
        distanceToEnd: scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight,
        viewport: scroller.clientHeight,
        heldFor: heldSince === null ? 0 : now - heldSince,
      });
      if (next === null) {
        heldSince = null;
        if (release) clearTimeout(release);
        release = null;
      } else if (heldSince === null) {
        heldSince = now;
        // Nothing may move for a while after the thought folds: the space is given back on time anyway.
        release = setTimeout(() => {
          release = null;
          hold();
        }, HOLD_MS);
      }
      if (next === held.current) return;
      held.current = next;
      content.style.minHeight = next === null ? '' : `${next}px`;
    };

    const remember = () => {
      const top = scroller.getBoundingClientRect().top;
      const atEnd = isAtEnd(scroller.scrollHeight, scroller.scrollTop, scroller.clientHeight);
      anchor.current = takeAnchor(atEnd, atEnd ? [] : entriesAround(content, top), top);
    };

    const correct = () => {
      if (!visible() || recent()) return;
      const current = anchor.current;
      if (current.atEnd) {
        if (!isAtEnd(scroller.scrollHeight, scroller.scrollTop, scroller.clientHeight)) toEndRef.current();
        return;
      }
      if (!current.entryId) return;
      const element = content.querySelector<HTMLElement>(`[data-entry-id="${CSS.escape(current.entryId)}"]`);
      const top = scroller.getBoundingClientRect().top;
      const delta = anchorCorrection(current, element ? element.getBoundingClientRect().top : null, top);
      if (delta !== null) scroller.scrollTop += delta;
    };
    correctRef.current = correct;

    const onScroll = () => {
      if (!visible()) return;
      const height = content.offsetHeight;
      if (height !== lastHeight) {
        lastHeight = height;
        if (!recent()) {
          correct();
          hold();
          return;
        }
      }
      remember();
      hold();
    };
    const onGesture = () => {
      gestureAt = performance.now();
    };
    const resize = new ResizeObserver(() => {
      if (!visible()) return;
      lastHeight = content.offsetHeight;
      correct();
      hold();
    });

    resize.observe(content);
    scroller.addEventListener('scroll', onScroll, { passive: true });
    for (const gesture of GESTURES) scroller.addEventListener(gesture, onGesture, { passive: true });
    return () => {
      if (release) clearTimeout(release);
      resize.disconnect();
      scroller.removeEventListener('scroll', onScroll);
      for (const gesture of GESTURES) scroller.removeEventListener(gesture, onGesture);
    };
  }, [scrollRef, contentRef, ready]);

  // A render that changed the entries puts the reader back before the frame is painted.
  useLayoutEffect(() => {
    correctRef.current();
  }, [version]);
};
