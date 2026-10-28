// The red dot that follows the pointer over the slide. The board reports the pointer in its own pixels (tk:deck-pointer);
// the dot is placed over the slide's box on screen. Positions go to CSS variables of the dot, not to React state.
import { useEffect, type RefObject } from 'react';
import type { IDeckPlayHost } from '@/next/features/designly/deck/deck-play';

export const useDeckLaser = (
  host: IDeckPlayHost,
  on: boolean,
  root: RefObject<HTMLElement | null>,
  dot: RefObject<HTMLElement | null>,
) => {
  const { setPointer, onPointer, slideRect, slides, index } = host;
  const slide = slides[index];
  const w = slide?.w ?? 0;
  const h = slide?.h ?? 0;
  useEffect(() => {
    if (!on || w <= 0 || h <= 0) return;
    const el = dot.current;
    setPointer(true);
    const off = onPointer((p) => {
      const rect = slideRect();
      const box = root.current?.getBoundingClientRect();
      if (!rect || !box || !el) return;
      el.style.setProperty('--x', `${rect.left - box.left + (p.x / w) * rect.width}px`);
      el.style.setProperty('--y', `${rect.top - box.top + (p.y / h) * rect.height}px`);
      el.dataset.live = '1';
    });
    return () => {
      off();
      setPointer(false);
      if (el) delete el.dataset.live;
    };
  }, [on, w, h, setPointer, onPointer, slideRect, root, dot]);
};
