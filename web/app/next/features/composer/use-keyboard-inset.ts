// Keeps the dock over the phone's on-screen keyboard: the part of the layout viewport the keyboard covers
// (visualViewport) goes to --hx-kb on the dock, which lifts it by that much. Desktop never sees a change.
import { useEffect, type RefObject } from 'react';

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
        const covered = Math.max(0, window.innerHeight - viewport.height - viewport.offsetTop);
        element.style.setProperty('--hx-kb', `${Math.round(covered)}px`);
        element.dataset.keyboard = covered > 80 ? 'open' : 'closed';
      });
    };
    update();
    viewport.addEventListener('resize', update);
    viewport.addEventListener('scroll', update);
    return () => {
      cancelAnimationFrame(frame);
      viewport.removeEventListener('resize', update);
      viewport.removeEventListener('scroll', update);
      element.style.removeProperty('--hx-kb');
    };
  }, [ref, holder]);
};

export default useKeyboardInset;
