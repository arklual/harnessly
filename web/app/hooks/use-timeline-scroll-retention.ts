import { useLayoutEffect, useMemo, type RefObject } from 'react';
import { isAtEnd } from '@/lib/timeline-scroll-anchor';

export const createTimelineScrollMemory = () => {
  let savedTop: number | null = null;
  // A reader at the end comes back to the end: replies that arrived while the tab was hidden made the old
  // position a place above it, and the timeline seemed to scroll up by itself.
  let savedAtEnd = false;
  return {
    attach(element: HTMLElement, stopScrolling: () => void) {
      let frame: number | undefined;
      let restoring = savedTop !== null;
      const target = savedTop;
      const cancelRestore = () => {
        if (frame !== undefined) cancelAnimationFrame(frame);
        restoring = false;
      };
      const remember = () => {
        // Activity's display:none can emit a synthetic scroll-to-zero. Keep
        // the last visible position instead of treating it as a user gesture.
        if (!restoring && element.clientHeight > 0) {
          savedTop = element.scrollTop;
          savedAtEnd = isAtEnd(element.scrollHeight, element.scrollTop, element.clientHeight);
        }
      };
      const restore = () => {
        if (!restoring || target === null || element.clientHeight === 0) return;
        if (savedAtEnd) {
          element.scrollTop = element.scrollHeight;
          return;
        }
        stopScrolling();
        element.scrollTop = target;
      };

      element.addEventListener('scroll', remember, { passive: true });
      const gestures = ['wheel', 'touchstart', 'pointerdown', 'keydown'];
      for (const gesture of gestures) element.addEventListener(gesture, cancelRestore, { passive: true });
      if (restoring) {
        restore();
        // Reapply after the scroll library's resize observer has seen reveal.
        frame = requestAnimationFrame(() => {
          restore();
          frame = requestAnimationFrame(() => {
            restore();
            restoring = false;
          });
        });
      }
      return () => {
        remember();
        cancelRestore();
        element.removeEventListener('scroll', remember);
        for (const gesture of gestures) element.removeEventListener(gesture, cancelRestore);
      };
    },
  };
};

export const useTimelineScrollRetention = (
  scrollRef: RefObject<HTMLElement | null>,
  sessionId: string | null,
  stopScrolling: () => void,
  enabled: boolean,
) => {
  const memory = useMemo(() => ({ sessionId, position: createTimelineScrollMemory() }), [sessionId]);
  // React Activity cleans up layout effects when hidden and recreates them
  // when shown, while keeping this session's memory alive.
  useLayoutEffect(() => {
    const element = scrollRef.current;
    if (!enabled || !element) return;
    return memory.position.attach(element, stopScrolling);
  }, [memory, scrollRef, stopScrolling, enabled]);
};
