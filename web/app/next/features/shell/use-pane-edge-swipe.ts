import { useEffect, useEffectEvent } from 'react';
import {
  AXIS_BIAS,
  PHONE_QUERY,
  SLOP_PX,
  ownsSideways,
  track,
  type ITrack,
} from '@/next/features/shell/gestures';

/** The strip at the screen's right edge left to the system's «back» gesture, px. */
export const SYSTEM_EDGE_PX = 20;
/** The swipe starts in this right share of the chat (outside the system's strip). */
const START_SHARE = 0.34;
/** How far left the finger goes for the pane to come, px; a quick flick needs less. */
const OPEN_PX = 56;
const FLICK_PX = 24;
const FLICK_VELOCITY = -0.35;

/**
 * On the phone a swipe over the chat from its right side towards the middle brings the right pane up: the
 * mirror of the swipe from the left that opens the chats. The last 20 px at the edge stay the system's «back».
 */
const usePaneEdgeSwipe = (onOpen: (() => void) | null) => {
  const open = useEffectEvent(() => onOpen?.());
  const enabled = onOpen !== null;
  useEffect(() => {
    if (!enabled) return;
    const phone = window.matchMedia(PHONE_QUERY);
    let drag:
      (ITrack & { pointerId: number; startX: number; startY: number; axis: 'x' | 'y' | null }) | null = null;

    const onDown = (event: PointerEvent) => {
      if (!phone.matches || event.pointerType === 'mouse' || drag || !event.isPrimary) return;
      const width = window.innerWidth;
      if (event.clientX < width * (1 - START_SHARE) || event.clientX > width - SYSTEM_EDGE_PX) return;
      const target = event.target instanceof Element ? event.target : null;
      if (!target?.closest('.hx-main') || target.closest('.hx-pane, .hx-header') || ownsSideways(target))
        return;
      if (document.querySelector('.hx-app[data-drawer="open"]')) return;
      drag = {
        pointerId: event.pointerId,
        startX: event.clientX,
        startY: event.clientY,
        axis: null,
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
        drag.axis = Math.abs(dx) > Math.abs(dy) * AXIS_BIAS && dx < 0 ? 'x' : 'y';
        if (drag.axis === 'y') {
          drag = null;
          return;
        }
      }
      track(drag, event.clientX, event.timeStamp);
    };
    const onUp = (event: PointerEvent) => {
      if (!drag || event.pointerId !== drag.pointerId) return;
      const current = drag;
      drag = null;
      if (current.axis !== 'x' || event.type !== 'pointerup') return;
      const dx = event.clientX - current.startX;
      if (dx <= -OPEN_PX || (dx <= -FLICK_PX && current.velocity < FLICK_VELOCITY)) open();
    };
    document.addEventListener('pointerdown', onDown, { passive: true });
    document.addEventListener('pointermove', onMove, { passive: true });
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
    return () => {
      document.removeEventListener('pointerdown', onDown);
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
    };
  }, [enabled]);
};

export default usePaneEdgeSwipe;
