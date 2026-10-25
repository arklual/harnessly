import { useEffect, type RefObject } from 'react';
import {
  AXIS_BIAS,
  PHONE_QUERY,
  SLOP_PX,
  ownsSideways,
  track,
  type ITrack,
} from '@/next/features/shell/gestures';

/** Where the flick would land after the finger leaves. */
const PROJECTION_MS = 220;
/** Past this share of its width the sheet goes. */
const CLOSE_AT = 0.35;

interface IDrag extends ITrack {
  pointerId: number;
  startX: number;
  startY: number;
  width: number;
  axis: 'x' | 'y' | null;
  x: number;
  frame: number;
}

/**
 * The right pane on the phone is a sheet over the chat that a finger pushes back to the right: `--px` on the sheet
 * and `--sp` on its scrim are written once a frame; on release it either closes from where it was let go
 * (`data-gclose`) or springs back.
 */
const usePaneSwipe = (
  pane: RefObject<HTMLElement | null>,
  scrim: RefObject<HTMLElement | null>,
  open: boolean,
  onClose: () => void,
) => {
  useEffect(() => {
    const paneEl = pane.current;
    if (!open || !paneEl) return;
    const scrimEl = scrim.current;
    paneEl.style.removeProperty('--px');
    paneEl.removeAttribute('data-gclose');
    const phone = window.matchMedia(PHONE_QUERY);
    let drag: IDrag | null = null;

    const paint = () => {
      if (!drag) return;
      drag.frame = 0;
      paneEl.style.setProperty('--px', `${drag.x.toFixed(1)}px`);
      scrimEl?.style.setProperty('--sp', Math.max(0, Math.min(1, 1 - drag.x / drag.width)).toFixed(3));
    };

    const release = (keepOffset: boolean) => {
      if (drag?.frame) cancelAnimationFrame(drag.frame);
      drag = null;
      paneEl.removeAttribute('data-sdrag');
      scrimEl?.removeAttribute('data-sdrag');
      scrimEl?.style.removeProperty('--sp');
      if (!keepOffset) paneEl.style.removeProperty('--px');
    };

    const onDown = (event: PointerEvent) => {
      if (!phone.matches || event.pointerType === 'mouse' || drag || !event.isPrimary) return;
      if (ownsSideways(event.target)) return;
      drag = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        width: paneEl.getBoundingClientRect().width || window.innerWidth,
        axis: null,
        x: 0,
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
        if (drag.axis === 'y' || dx < 0) {
          drag = null;
          return;
        }
        paneEl.setAttribute('data-sdrag', '');
        scrimEl?.setAttribute('data-sdrag', '');
      }
      track(drag, event.clientX, event.timeStamp);
      drag.x = Math.max(0, dx);
      if (!drag.frame) drag.frame = requestAnimationFrame(paint);
    };

    const onUp = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.pointerId) return;
      const current = drag;
      if (current.axis !== 'x' || event.type !== 'pointerup') {
        release(false);
        return;
      }
      const gone = current.x + current.velocity * PROJECTION_MS > current.width * CLOSE_AT;
      release(gone);
      if (gone) {
        paneEl.setAttribute('data-gclose', '');
        onClose();
      }
    };

    paneEl.addEventListener('pointerdown', onDown, { passive: true });
    document.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    return () => {
      paneEl.removeEventListener('pointerdown', onDown);
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
      // A sheet let go past the line closes from where it is: its exit starts at the offset it was dragged to.
      release(true);
    };
  }, [pane, scrim, open, onClose]);
};

export default usePaneSwipe;
