// Keeps the dock over the phone's on-screen keyboard: the part of the layout viewport the keyboard covers
// (visualViewport) goes to --hx-kb on the dock, which lifts it by that much. Desktop never sees a change.
import { useEffect, type RefObject } from 'react';

export type ViewportSample = {
  innerHeight: number;
  height: number;
  offsetTop: number;
  scale: number;
  editing: boolean;
};

/** A pinch-zoomed page shrinks the visual viewport too; past this scale the gap is zoom, not a keyboard. */
const ZOOM_SCALE = 1.01;

/**
 * How many pixels of the layout viewport the on-screen keyboard covers. Only a keyboard counts: nothing while
 * no editable field is focused (no keyboard can be up) and nothing while the page is pinch-zoomed.
 */
export const keyboardInset = ({ innerHeight, height, offsetTop, scale, editing }: ViewportSample) => {
  if (!editing || scale > ZOOM_SCALE) return 0;
  return Math.max(0, Math.round(innerHeight - height - offsetTop));
};

const isEditing = (element: Element | null) =>
  element instanceof HTMLElement &&
  (element.isContentEditable ||
    element instanceof HTMLTextAreaElement ||
    element instanceof HTMLInputElement);

/** `holder` is the element that gets lifted: the closest ancestor of `ref` (or itself) matching it. */
const useKeyboardInset = (ref: RefObject<HTMLElement | null>, holder = '.hx-dock') => {
  useEffect(() => {
    const viewport = window.visualViewport;
    const element = ref.current?.closest<HTMLElement>(holder) ?? null;
    if (!viewport || !element) return;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const covered = keyboardInset({
          innerHeight: window.innerHeight,
          height: viewport.height,
          offsetTop: viewport.offsetTop,
          scale: viewport.scale,
          editing: isEditing(document.activeElement),
        });
        element.style.setProperty('--hx-kb', `${covered}px`);
        element.dataset.keyboard = covered > 80 ? 'open' : 'closed';
      });
    };
    update();
    viewport.addEventListener('resize', update);
    viewport.addEventListener('scroll', update);
    document.addEventListener('focusin', update);
    document.addEventListener('focusout', update);
    return () => {
      cancelAnimationFrame(frame);
      viewport.removeEventListener('resize', update);
      viewport.removeEventListener('scroll', update);
      document.removeEventListener('focusin', update);
      document.removeEventListener('focusout', update);
      element.style.removeProperty('--hx-kb');
    };
  }, [ref, holder]);
};

export default useKeyboardInset;
