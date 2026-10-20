import { useLayoutEffect, type RefObject } from 'react';
import { computePlacement, pointRect, type IRect, type TLayerAlign, type TLayerSide } from './placement';

/** A layer is anchored either to an element or to a pointer position (context menu). */
export interface ITwPoint {
  x: number;
  y: number;
}

export type TTwAnchor = RefObject<HTMLElement | null> | ITwPoint;

export const isPointAnchor = (anchor: TTwAnchor): anchor is ITwPoint => 'x' in anchor && 'y' in anchor;

export interface IAnchoredPositionOptions {
  enabled: boolean;
  side: TLayerSide;
  align: TLayerAlign;
  offset: number;
  margin: number;
}

const anchorRect = (anchor: TTwAnchor): IRect | null => {
  if (isPointAnchor(anchor)) return pointRect(anchor.x, anchor.y);
  const element = anchor.current;
  if (!element) return null;
  const rect = element.getBoundingClientRect();
  return { top: rect.top, left: rect.left, width: rect.width, height: rect.height };
};

const viewportSize = () => {
  const visual = window.visualViewport;
  return {
    width: document.documentElement.clientWidth,
    height: visual ? Math.min(visual.height, window.innerHeight) : window.innerHeight,
  };
};

/**
 * Places `floatingRef` next to `anchor` with position: fixed. Coordinates go into CSS custom
 * properties (--tw-ui-layer-x/-y/-max-h/-max-w/-anchor-w) consumed by hl-ui.css, never into
 * a style attribute written by JSX. Follows scroll, resize, the on-screen keyboard and size changes.
 */
export const useAnchoredPosition = (
  floatingRef: RefObject<HTMLElement | null>,
  anchor: TTwAnchor,
  options: IAnchoredPositionOptions,
): void => {
  const { enabled, side, align, offset, margin } = options;

  useLayoutEffect(() => {
    if (!enabled) return;
    const floating = floatingRef.current;
    if (!floating) return;
    let frame = 0;

    const update = () => {
      frame = 0;
      const rect = anchorRect(anchor);
      if (!rect) return;
      const placement = computePlacement({
        anchor: rect,
        floating: {
          width: floating.offsetWidth,
          height: Math.max(floating.offsetHeight, floating.scrollHeight),
        },
        viewport: viewportSize(),
        side,
        align,
        offset,
        margin,
      });
      floating.style.setProperty('--tw-ui-layer-x', `${placement.left}px`);
      floating.style.setProperty('--tw-ui-layer-y', `${placement.top}px`);
      floating.style.setProperty('--tw-ui-layer-max-h', `${placement.maxHeight}px`);
      floating.style.setProperty('--tw-ui-layer-max-w', `${placement.maxWidth}px`);
      floating.style.setProperty('--tw-ui-anchor-w', `${Math.round(rect.width)}px`);
      floating.dataset.side = placement.side;
      floating.dataset.placed = 'true';
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };

    update();
    window.addEventListener('resize', schedule);
    window.addEventListener('scroll', schedule, true);
    window.visualViewport?.addEventListener('resize', schedule);
    const observer = new ResizeObserver(schedule);
    observer.observe(floating);
    if (!isPointAnchor(anchor) && anchor.current) observer.observe(anchor.current);

    return () => {
      if (frame) cancelAnimationFrame(frame);
      window.removeEventListener('resize', schedule);
      window.removeEventListener('scroll', schedule, true);
      window.visualViewport?.removeEventListener('resize', schedule);
      observer.disconnect();
    };
  }, [align, anchor, enabled, floatingRef, margin, offset, side]);
};
