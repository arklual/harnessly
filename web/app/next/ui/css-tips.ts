// The CSS hints (.hx-tipped > .hx-tip, ui.css) on the same rules as the Tooltip component: the same 450 ms, no wait
// when the pointer comes from another hint that was showing (the row reads as one, nothing flickers), and never over
// what they explain: a hint that has no room on its side flips to the other and slides back inside the window. One
// listener on the interface's root; the hints stay plain CSS.
import { useEffect, type RefObject } from 'react';
import { ALIGN, HOVER, placeFloating, rectOf, SIDE, TIP_WARMTH, type TAlign } from '@/next/ui/hover-intent';

const TIPPED = '.hx-tipped';

const alignOf = (tip: HTMLElement): TAlign => {
  const align = tip.dataset.align;
  if (align === ALIGN.start || align === ALIGN.end) return align;
  return ALIGN.center;
};

/** Puts the hint on the side with room and inside the window: `data-flip` and `--tip-dx`, read by ui.css. */
export const placeCssTip = (tipped: HTMLElement): void => {
  const tip = tipped.querySelector<HTMLElement>(':scope > .hx-tip');
  if (!tip) return;
  const preferred = tip.dataset.pos === 'below' ? SIDE.bottom : SIDE.top;
  const anchor = rectOf(tipped);
  const size = { width: tip.offsetWidth, height: tip.offsetHeight };
  const align = alignOf(tip);
  const place = placeFloating({
    anchor,
    size,
    viewport: { width: window.innerWidth, height: window.innerHeight },
    side: preferred,
    align,
  });
  if (place.side === preferred) tip.removeAttribute('data-flip');
  else tip.dataset.flip = place.side;
  const unclamped = {
    [ALIGN.start]: anchor.left,
    [ALIGN.center]: (anchor.left + anchor.right) / 2 - size.width / 2,
    [ALIGN.end]: anchor.right - size.width,
  } as const;
  const dx = Math.round(place.x - unclamped[align]);
  if (dx === 0) tip.style.removeProperty('--tip-dx');
  else tip.style.setProperty('--tip-dx', `${dx}px`);
};

/** Follows the pointer over the root's CSS hints: warmth (`data-tips-warm` on the root) and placement. */
export const useCssTips = (rootRef: RefObject<HTMLElement | null>): void => {
  useEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    let current: HTMLElement | null = null;
    let since = 0;
    let showing = false;

    const leaveCurrent = () => {
      // The hint was on screen (it waited out its delay, or skipped it): the next one comes at once.
      if (current && (showing || Date.now() - since >= HOVER.tipDelay)) TIP_WARMTH.hidden();
      showing = false;
      current = null;
    };

    const onOver = (event: PointerEvent) => {
      if (event.pointerType !== 'mouse') return;
      const target = event.target instanceof Element ? event.target.closest<HTMLElement>(TIPPED) : null;
      if (target === current) return;
      leaveCurrent();
      if (!target || !root.contains(target)) return;
      const warm = TIP_WARMTH.isWarm();
      root.toggleAttribute('data-tips-warm', warm);
      current = target;
      since = Date.now();
      showing = warm;
      placeCssTip(target);
    };
    const onOut = (event: PointerEvent) => {
      if (event.relatedTarget === null) leaveCurrent();
    };

    root.addEventListener('pointerover', onOver);
    root.addEventListener('pointerout', onOut);
    return () => {
      root.removeEventListener('pointerover', onOver);
      root.removeEventListener('pointerout', onOut);
    };
  }, [rootRef]);
};
