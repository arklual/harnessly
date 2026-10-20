import { useEffect, useEffectEvent } from 'react';
import type { TSwipeDirection } from '@/lib/hl-frame-bridge';

const AXIS_LOCK_PX = 12;
const SWIPE_MIN_PX = 64;
const KEYBOARD_DRAG_PX = 36;

interface IStart {
  x: number;
  y: number;
  target: EventTarget | null;
}

const isEditable = (el: Element | null): el is HTMLElement =>
  el instanceof HTMLTextAreaElement ||
  (el instanceof HTMLInputElement && !['button', 'checkbox', 'radio', 'range', 'submit'].includes(el.type)) ||
  (el instanceof HTMLElement && el.isContentEditable);

/** Something under the finger scrolls sideways in that direction (code, tables, the terminal): the swipe is its. */
const ownsSideways = (target: EventTarget | null, dx: number) => {
  for (
    let el = target instanceof Element ? target : null;
    el && el !== document.body;
    el = el.parentElement
  ) {
    if (el.classList.contains('xterm')) return true;
    if (el.scrollWidth <= el.clientWidth + 1) continue;
    const overflow = getComputedStyle(el).overflowX;
    if (overflow !== 'auto' && overflow !== 'scroll') continue;
    if (dx > 0 ? el.scrollLeft > 0 : el.scrollLeft + el.clientWidth < el.scrollWidth - 1) return true;
  }
  return false;
};

/**
 * Phone gestures of the workspace: a sideways swipe reports its direction, and dragging down
 * while typing puts the keyboard away, as in iOS Messages. Listeners are passive, scrolling is untouched.
 */
const useTouchGestures = (enabled: boolean, onSwipe: (direction: TSwipeDirection) => void) => {
  const swipe = useEffectEvent(onSwipe);

  useEffect(() => {
    if (!enabled) return;
    let start: IStart | null = null;
    let axis: 'x' | 'y' | null = null;
    let keyboardDismissed = false;

    const down = (event: TouchEvent) => {
      const touch = event.touches[0];
      // Dialogs and pannable content (zoomed pictures, canvases) keep their own drags.
      const own =
        event.target instanceof Element &&
        event.target.closest('[role="dialog"], [data-swipe-ignore], .tw-zoom');
      start =
        event.touches.length === 1 && touch && !own
          ? { x: touch.clientX, y: touch.clientY, target: event.target }
          : null;
      axis = null;
      keyboardDismissed = false;
    };
    const move = (event: TouchEvent) => {
      const touch = event.touches[0];
      if (!start || !touch) return;
      const dx = touch.clientX - start.x;
      const dy = touch.clientY - start.y;
      if (!axis) {
        if (Math.abs(dx) < AXIS_LOCK_PX && Math.abs(dy) < AXIS_LOCK_PX) return;
        axis = Math.abs(dx) > Math.abs(dy) * 1.3 ? 'x' : 'y';
        if (axis === 'x' && ownsSideways(start.target, dx)) start = null;
        return;
      }
      if (axis !== 'y' || keyboardDismissed || dy < KEYBOARD_DRAG_PX) return;
      const active = document.activeElement;
      if (!isEditable(active)) return;
      // Dragging inside a field that has more text than fits scrolls that text instead.
      if (active.contains(start.target as Node) && active.scrollHeight > active.clientHeight) return;
      active.blur();
      keyboardDismissed = true;
    };
    const up = (event: TouchEvent) => {
      const touch = event.changedTouches[0];
      const began = start;
      start = null;
      if (!began || axis !== 'x' || !touch) return;
      const dx = touch.clientX - began.x;
      const dy = touch.clientY - began.y;
      if (Math.abs(dx) >= SWIPE_MIN_PX && Math.abs(dx) > Math.abs(dy) * 1.5) swipe(dx > 0 ? 'right' : 'left');
    };
    const cancel = () => {
      start = null;
    };

    const options = { capture: true, passive: true } as const;
    window.addEventListener('touchstart', down, options);
    window.addEventListener('touchmove', move, options);
    window.addEventListener('touchend', up, options);
    window.addEventListener('touchcancel', cancel, options);
    return () => {
      window.removeEventListener('touchstart', down, options);
      window.removeEventListener('touchmove', move, options);
      window.removeEventListener('touchend', up, options);
      window.removeEventListener('touchcancel', cancel, options);
    };
  }, [enabled]);
};

export default useTouchGestures;
