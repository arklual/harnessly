import { useEffect, useEffectEvent, type RefObject } from 'react';
import { PHONE_QUERY } from '@/next/features/shell/gestures';

/** The sheet's two places on the phone: most of the screen, or all of it. */
export const PANE_DETENT = { half: 'half', full: 'full' } as const;
export type TPaneDetent = (typeof PANE_DETENT)[keyof typeof PANE_DETENT];

/** The half sheet leaves this share of the screen's height above it (it covers about two thirds). */
const HALF_GAP = 0.34;
/** Where the flick would land after the finger leaves. */
const PROJECTION_MS = 200;
/** Movement before a touch is read as a drag of the sheet (or left to what is under it), px. */
const SLOP_PX = 6;
/** Pulled above the full sheet, the finger moves it this much: it resists. */
const RUBBER = 0.18;

/** Where the sheet's handle is: the grip and the pane's bar of tabs. A drag there always moves the sheet. */
const HANDLE = '.hx-pane__grip, .hx-pane__bar, .hx-pane > .hx-strip, .hx-pane .hx-strip';
/** What owns a finger whatever the sheet: a field, a canvas, a terminal, a frame, a horizontal strip. */
const OWNS_TOUCH =
  'input, textarea, [contenteditable="true"], [role="slider"], canvas, iframe, .xterm, [data-nogesture]';

/** The offset of the half sheet from the full one, px. */
const halfOffset = (): number => Math.round(window.innerHeight * HALF_GAP);

/** A box between the touch and the sheet that is scrolled: the finger scrolls it, it does not move the sheet. */
const scrolledBetween = (from: Element, sheet: HTMLElement): boolean => {
  for (let node: Element | null = from; node && node !== sheet; node = node.parentElement) {
    if (node.scrollTop > 0 && node.scrollHeight > node.clientHeight + 1) return true;
  }
  return false;
};

/**
 * The right pane on the phone is a bottom sheet with two places and a close: it rises to about two thirds
 * of the screen (a design: to the whole screen at once), a drag up opens it to the whole screen, a drag down folds it
 * to two thirds, and once more closes it. The finger moves it through `--pane-dy`, written straight into the element
 * (no React render); the place is `--pane-ty` and `data-detent`. On release it settles at the place the flick would
 * land nearest to, with the sheet's spring (styles/shell.css).
 *
 * The handle (the grip, the bar of tabs) always drags it; the content drags it only down and only while the content
 * under the finger is scrolled to its top, so scrolling a list never moves the sheet. Touch only.
 */
const usePaneSwipe = (
  pane: RefObject<HTMLElement | null>,
  scrim: RefObject<HTMLElement | null>,
  open: boolean,
  onClose: () => void,
  /** The sheet takes the whole screen (a design): it has no half place. */
  full = false,
) => {
  const close = useEffectEvent(onClose);

  // The place: full for a design, else two thirds on each opening; a design opened in an open sheet raises it.
  useEffect(() => {
    const paneEl = pane.current;
    if (!open || !paneEl) return;
    paneEl.style.removeProperty('--pane-dy');
    const set = (detent: TPaneDetent) => {
      paneEl.setAttribute('data-detent', detent);
      paneEl.style.setProperty('--pane-ty', detent === PANE_DETENT.full ? '0px' : `${halfOffset()}px`);
    };
    if (full || paneEl.getAttribute('data-detent') === null) set(full ? PANE_DETENT.full : PANE_DETENT.half);
    const onResize = () =>
      set(paneEl.getAttribute('data-detent') === PANE_DETENT.full ? PANE_DETENT.full : PANE_DETENT.half);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [pane, open, full]);

  useEffect(() => {
    const paneEl = pane.current;
    if (!open || !paneEl) return;
    const scrimEl = scrim.current;
    const phone = window.matchMedia(PHONE_QUERY);
    let drag: {
      startX: number;
      startY: number;
      base: number;
      handle: boolean;
      active: boolean;
      dy: number;
      lastY: number;
      lastT: number;
      velocity: number;
      frame: number;
    } | null = null;

    const paint = () => {
      if (!drag) return;
      drag.frame = 0;
      paneEl.style.setProperty('--pane-dy', `${drag.dy.toFixed(1)}px`);
      const half = halfOffset();
      const at = drag.base + drag.dy;
      // The scrim fades only below the half place, on the way out.
      const shade = at <= half ? 1 : Math.max(0, 1 - (at - half) / (window.innerHeight - half));
      scrimEl?.style.setProperty('--sp', shade.toFixed(3));
    };

    const release = () => {
      if (drag?.frame) cancelAnimationFrame(drag.frame);
      drag = null;
      paneEl.removeAttribute('data-sdrag');
      scrimEl?.removeAttribute('data-sdrag');
      scrimEl?.style.removeProperty('--sp');
      paneEl.style.removeProperty('--pane-dy');
    };

    const onStart = (event: TouchEvent) => {
      if (!phone.matches || drag || event.touches.length !== 1) return;
      const target = event.target instanceof Element ? event.target : null;
      if (!target || target.closest(OWNS_TOUCH)) return;
      const handle = target.closest(HANDLE) !== null;
      if (!handle && scrolledBetween(target, paneEl)) return;
      const touch = event.touches[0];
      drag = {
        startX: touch.clientX,
        startY: touch.clientY,
        base: paneEl.getAttribute('data-detent') === PANE_DETENT.full ? 0 : halfOffset(),
        handle,
        active: false,
        dy: 0,
        lastY: touch.clientY,
        lastT: event.timeStamp,
        velocity: 0,
        frame: 0,
      };
    };

    const onMove = (event: TouchEvent) => {
      if (!drag) return;
      const touch = event.touches[0];
      const dx = touch.clientX - drag.startX;
      const dy = touch.clientY - drag.startY;
      if (!drag.active) {
        if (Math.abs(dx) < SLOP_PX && Math.abs(dy) < SLOP_PX) return;
        // Sideways (a strip of tabs, a slider), or up over the content (it scrolls): not the sheet's.
        if (Math.abs(dx) > Math.abs(dy) || (!drag.handle && dy < 0)) {
          drag = null;
          return;
        }
        drag.active = true;
        drag.startY = touch.clientY;
        paneEl.setAttribute('data-sdrag', '');
        scrimEl?.setAttribute('data-sdrag', '');
      }
      if (event.cancelable) event.preventDefault();
      const dt = event.timeStamp - drag.lastT;
      if (dt > 0) drag.velocity = (touch.clientY - drag.lastY) / dt;
      drag.lastY = touch.clientY;
      drag.lastT = event.timeStamp;
      const raw = touch.clientY - drag.startY;
      // Above the full place the sheet resists.
      const over = drag.base + raw;
      drag.dy = over < 0 ? -drag.base + over * RUBBER : raw;
      if (!drag.frame) drag.frame = requestAnimationFrame(paint);
    };

    const onEnd = (event: TouchEvent) => {
      if (!drag) return;
      const current = drag;
      if (!current.active || event.type !== 'touchend') {
        release();
        return;
      }
      const half = halfOffset();
      const landing = current.base + current.dy + current.velocity * PROJECTION_MS;
      const places = full ? [0, window.innerHeight] : [0, half, window.innerHeight];
      // A flick moves one place at most: from the full sheet a flick down folds it, it does not close it.
      const from = places.indexOf(current.base);
      let next = places.reduce((best, place) =>
        Math.abs(place - landing) < Math.abs(best - landing) ? place : best,
      );
      const step = places.indexOf(next) - from;
      if (Math.abs(step) > 1) next = places[from + Math.sign(step)];
      if (next === window.innerHeight) {
        // It closes from where it was let go: the exit starts at that offset.
        if (drag?.frame) cancelAnimationFrame(drag.frame);
        drag = null;
        paneEl.removeAttribute('data-sdrag');
        scrimEl?.removeAttribute('data-sdrag');
        paneEl.style.setProperty('--pane-ty', `${current.base}px`);
        close();
        return;
      }
      paneEl.setAttribute('data-detent', next === 0 ? PANE_DETENT.full : PANE_DETENT.half);
      paneEl.style.setProperty('--pane-ty', `${next}px`);
      release();
    };

    paneEl.addEventListener('touchstart', onStart, { passive: true });
    paneEl.addEventListener('touchmove', onMove, { passive: false });
    paneEl.addEventListener('touchend', onEnd);
    paneEl.addEventListener('touchcancel', onEnd);
    return () => {
      paneEl.removeEventListener('touchstart', onStart);
      paneEl.removeEventListener('touchmove', onMove);
      paneEl.removeEventListener('touchend', onEnd);
      paneEl.removeEventListener('touchcancel', onEnd);
      if (drag?.frame) cancelAnimationFrame(drag.frame);
      drag = null;
    };
  }, [pane, scrim, open, full]);
};

export default usePaneSwipe;
