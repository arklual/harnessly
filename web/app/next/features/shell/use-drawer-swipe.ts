import { useEffect, type RefObject } from 'react';
import {
  AXIS_BIAS,
  PHONE_QUERY,
  SLOP_PX,
  ownsSideways,
  track,
  type ITrack,
} from '@/next/features/shell/gestures';

/** The flick carries on for this long after the finger leaves: a short quick swipe still opens or closes. */
const PROJECTION_MS = 200;

interface IDrag extends ITrack {
  pointerId: number;
  kind: 'open' | 'close';
  startX: number;
  startY: number;
  width: number;
  axis: 'x' | 'y' | null;
  progress: number;
  frame: number;
}

interface IDrawerRefs {
  /** The app; its `.hx-sidebar` is the drawer. */
  app: RefObject<HTMLElement | null>;
  scrim: RefObject<HTMLElement | null>;
}

/**
 * The phone drawer follows a finger: a sideways drag over the chat pulls it out from the left, a drag left over the
 * open drawer (or its scrim) pushes it back. While the finger moves, the drawer's transform and the scrim's opacity
 * are written straight into those two elements once a frame (no React render, no restyle of the app); on release the
 * drawer settles where the flick would land, with the CSS ease-out, and only then the open state changes.
 */
const useDrawerSwipe = ({ app, scrim }: IDrawerRefs, open: boolean, setOpen: (open: boolean) => void) => {
  useEffect(() => {
    const appEl = app.current;
    const sidebarEl = appEl?.querySelector<HTMLElement>('.hx-sidebar');
    const scrimEl = scrim.current;
    if (!appEl || !sidebarEl || !scrimEl) return;
    const phone = window.matchMedia(PHONE_QUERY);
    let drag: IDrag | null = null;

    const paint = () => {
      if (!drag) return;
      drag.frame = 0;
      sidebarEl.style.transform = `translate3d(calc(${((drag.progress - 1) * 100).toFixed(2)}% - 1px), 0, 0)`;
      scrimEl.style.opacity = drag.progress.toFixed(3);
    };

    const release = () => {
      if (drag?.frame) cancelAnimationFrame(drag.frame);
      drag = null;
      appEl.removeAttribute('data-ddrag');
      sidebarEl.style.removeProperty('transform');
      scrimEl.style.removeProperty('opacity');
    };

    const onDown = (event: PointerEvent) => {
      if (!phone.matches || event.pointerType === 'mouse' || drag || !event.isPrimary) return;
      const target = event.target as Element;
      let kind: IDrag['kind'] | null = null;
      if (open && target.closest('.hx-sidebar, .hx-scrim')) kind = 'close';
      else if (!open && target.closest('.hx-main') && !target.closest('.hx-pane') && !ownsSideways(target))
        kind = 'open';
      if (!kind) return;
      drag = {
        pointerId: event.pointerId,
        kind,
        startX: event.clientX,
        startY: event.clientY,
        width: sidebarEl.getBoundingClientRect().width || 300,
        axis: null,
        progress: open ? 1 : 0,
        frame: 0,
        lastX: event.clientX,
        lastT: event.timeStamp,
        velocity: 0,
      };
    };

    const onMove = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.pointerId) return;
      const dx = event.clientX - drag.startX;
      const dy = event.clientY - drag.startY;
      if (drag.axis === null) {
        if (Math.abs(dx) < SLOP_PX && Math.abs(dy) < SLOP_PX) return;
        drag.axis = Math.abs(dx) > Math.abs(dy) * AXIS_BIAS ? 'x' : 'y';
        if (drag.axis === 'y' || (drag.kind === 'open' && dx < 0)) {
          drag = null;
          return;
        }
        appEl.setAttribute('data-ddrag', '');
      }
      track(drag, event.clientX, event.timeStamp);
      const raw = drag.kind === 'open' ? dx / drag.width : 1 + dx / drag.width;
      drag.progress = Math.min(1, Math.max(0, raw));
      if (!drag.frame) drag.frame = requestAnimationFrame(paint);
    };

    const onUp = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.pointerId) return;
      const current = drag;
      const dragged = current.axis === 'x' && event.type === 'pointerup';
      release();
      if (!dragged) return;
      const landing = current.progress + (current.velocity * PROJECTION_MS) / current.width;
      setOpen(landing > 0.5);
    };

    // A drag that ends on another element (or outside the window) still ends: the listeners are on the document.
    document.addEventListener('pointerdown', onDown, { passive: true });
    document.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    return () => {
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
      release();
    };
  }, [app, scrim, open, setOpen]);
};

export default useDrawerSwipe;
