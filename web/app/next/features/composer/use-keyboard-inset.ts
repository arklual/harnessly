// Keeps the dock over the phone's on-screen keyboard: the part of the layout viewport the keyboard covers
// (visualViewport) goes to --hx-kb on the dock, which lifts it by that much. Desktop never sees a change.
import { useEffect, type RefObject } from 'react';

const useKeyboardInset = (ref: RefObject<HTMLElement | null>) => {
  useEffect(() => {
    const viewport = window.visualViewport;
    const element = ref.current?.closest<HTMLElement>('.hx-dock') ?? null;
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
  }, [ref]);
};

export default useKeyboardInset;
