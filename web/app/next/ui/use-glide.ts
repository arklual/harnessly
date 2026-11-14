// A box whose content changes at once (another chat's model, its limits, the right column of another chat) goes over
// from its old size to the new one instead of jumping. The size before is what a ResizeObserver last saw (no read of
// layout on a render); the size after is read once, when the new content is drawn (the key changed), and the change is
// played by the Web Animations API on that box alone. Nothing with reduced motion.
import { useEffect, useLayoutEffect, useRef } from 'react';

export const GLIDE_MS = 180;
const GLIDE_EASE = 'cubic-bezier(0.22, 1, 0.36, 1)';

const reducedMotion = (): boolean =>
  typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export const useGlide = (
  box: HTMLElement | null,
  key: string,
  axis: 'width' | 'height',
  ms = GLIDE_MS,
): void => {
  const size = useRef<number | null>(null);
  const shownKey = useRef(key);
  useEffect(() => {
    if (!box || typeof ResizeObserver !== 'function') return;
    const observer = new ResizeObserver(([entry]) => {
      const border = entry.borderBoxSize?.[0];
      if (border) size.current = axis === 'width' ? border.inlineSize : border.blockSize;
      else size.current = axis === 'width' ? entry.contentRect.width : entry.contentRect.height;
    });
    observer.observe(box);
    return () => observer.disconnect();
  }, [box, axis]);
  useLayoutEffect(() => {
    if (shownKey.current === key) return;
    shownKey.current = key;
    const start = size.current;
    if (!box || start === null || reducedMotion() || typeof box.animate !== 'function') return;
    const end = axis === 'width' ? box.offsetWidth : box.offsetHeight;
    if (Math.abs(start - end) < 1) return;
    box.animate([{ [axis]: `${start}px` }, { [axis]: `${end}px` }], { duration: ms, easing: GLIDE_EASE });
  }, [box, key, axis, ms]);
};
