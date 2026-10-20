import { useEffect, type RefObject } from 'react';

type TVisibilityHandler = (visible: boolean) => void;

const handlers = new Map<Element, TVisibilityHandler>();
let shared: IntersectionObserver | null = null;

/** One observer for every animated element on the page, however many rows and sparks are running. */
export const observeVisibility = (element: Element, onChange: TVisibilityHandler) => {
  if (typeof IntersectionObserver === 'undefined') return () => {};
  shared ??= new IntersectionObserver((entries) => {
    for (const entry of entries) handlers.get(entry.target)?.(entry.isIntersecting);
  });
  handlers.set(element, onChange);
  shared.observe(element);
  return () => {
    handlers.delete(element);
    shared?.unobserve(element);
  };
};

const OFFSCREEN = 'data-offscreen';

/**
 * Marks the element `data-offscreen` while it is scrolled out of view or hidden, so CSS can hold its animations there:
 * a working row below the fold of the sidebar or a stale tool call far up the timeline costs nothing.
 * The attribute is flipped in place; nothing re-renders.
 */
const usePauseOffscreen = (ref: RefObject<HTMLElement | null>, active: boolean) => {
  useEffect(() => {
    const element = ref.current;
    if (!active || !element) return;
    const stop = observeVisibility(element, (visible) => element.toggleAttribute(OFFSCREEN, !visible));
    return () => {
      stop();
      element.removeAttribute(OFFSCREEN);
    };
  }, [ref, active]);
};

export default usePauseOffscreen;
