// The notification centre's motion, on transform and opacity only. useFlip: when a row comes, leaves or grows, the
// rows and headings around it do not jump to their new places: each starts where it was and springs to where it is
// now. useSwipe: a row follows a finger, a mouse or two fingers on a trackpad sideways; let go past a third of its
// width (or flicked) it flies off and is taken away, short of that it springs back.
import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { releaseSpeed, softSpeed, swipeVerdict } from '@/next/ui/gesture-rules';

/** The spring of styles/notification-center.css (response 0.45 s, bounce 0.18), as long as it takes to settle. */
export const SPRING_MS = 580;
/** How long a row takes to fly off. */
export const LEAVE_MS = 220;

const reducedMotion = (): boolean =>
  typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;

/**
 * After each render, every `[data-flip]` element under the container that moved since the last one is put back where it
 * was (translateY, no transition) and let go to its place with the spring. One read of every position, then the writes.
 */
export const useFlip = (container: RefObject<HTMLElement | null>, active: boolean) => {
  const before = useRef(new Map<string, number>());
  useLayoutEffect(() => {
    const root = container.current;
    if (!root || !active) {
      before.current = new Map();
      return;
    }
    // A row in a swipe or on its way out keeps its own move: a spring from here would pull it back into the list.
    const nodes = [...root.querySelectorAll<HTMLElement>('[data-flip]')].filter(
      (node) => node.dataset.swiping === undefined && node.dataset.leaving === undefined,
    );
    const now = new Map<string, number>();
    const tops = nodes.map(
      (node) => node.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop,
    );
    const moved: [HTMLElement, number][] = [];
    nodes.forEach((node, i) => {
      const key = node.dataset.flip as string;
      const top = tops[i] as number;
      now.set(key, top);
      const was = before.current.get(key);
      if (was !== undefined && Math.abs(was - top) > 0.5) moved.push([node, was - top]);
    });
    before.current = now;
    if (moved.length === 0 || reducedMotion()) return;
    for (const [node, dy] of moved) {
      node.style.transition = 'none';
      node.style.transform = `translateY(${dy}px)`;
    }
    // One reflow for all of them, then the spring home.
    void root.offsetHeight;
    for (const [node] of moved) {
      node.style.transition = `transform ${SPRING_MS}ms var(--nc-spring)`;
      node.style.transform = '';
    }
  });
};

const SLOP_PX = 8;
const WHEEL_IDLE_MS = 140;

/**
 * Sweeps a row aside. The shift goes straight into the element's transform while the gesture leads (nothing
 * re-renders); `onGone` runs once the row has flown off. Vertical moves are the list's scroll, not the row's.
 */
export const useSwipe = (target: RefObject<HTMLElement | null>, enabled: boolean, onGone: () => void) => {
  const gone = useRef(onGone);
  useEffect(() => {
    gone.current = onGone;
  });

  useEffect(() => {
    const el = target.current;
    if (!el || !enabled) return;
    let start: { x: number; y: number; t: number; id: number } | null = null;
    let leading = false;
    let dx = 0;
    let last = { x: 0, t: 0 };
    let speed = 0;
    let wheel = 0;
    let wheelTimer = 0;
    let swallow = false;

    // The row's width, read once as the gesture starts: a read after every write would lay the page out on each move.
    let width = 0;
    let frame = 0;
    let shift = 0;
    const paint = () => {
      frame = 0;
      el.style.transform = shift === 0 ? '' : `translateX(${shift}px)`;
      el.style.opacity = shift === 0 ? '' : String(Math.max(0.2, 1 - Math.abs(shift) / width));
    };
    // The shift follows the gesture once a frame, however many moves a fast mouse or a trackpad sends in between.
    const set = (x: number) => {
      if (el.dataset.swiping === undefined) {
        el.dataset.swiping = '';
        el.style.transition = 'none';
        el.style.willChange = 'transform, opacity';
        width = el.offsetWidth || 1;
      }
      shift = x;
      if (frame === 0) frame = requestAnimationFrame(paint);
    };
    const still = () => {
      if (frame !== 0) cancelAnimationFrame(frame);
      frame = 0;
      el.style.willChange = '';
    };
    const back = () => {
      still();
      delete el.dataset.swiping;
      el.style.transition = `transform ${SPRING_MS}ms var(--nc-spring), opacity 200ms var(--hx-ease-out)`;
      el.style.transform = '';
      el.style.opacity = '';
    };
    const fly = (dir: number) => {
      still();
      el.dataset.swiping = '';
      el.style.transition = `transform ${LEAVE_MS}ms var(--hx-ease-out), opacity ${LEAVE_MS}ms var(--hx-ease-out)`;
      el.style.transform = `translateX(${dir * (el.offsetWidth + 24)}px)`;
      el.style.opacity = '0';
      window.setTimeout(() => gone.current(), reducedMotion() ? 0 : LEAVE_MS);
    };
    const settle = (shift: number, v: number) => {
      if (swipeVerdict(shift, width || el.offsetWidth, v, SLOP_PX) === 'fly') fly(Math.sign(shift) || -1);
      else back();
    };

    const onDown = (e: PointerEvent) => {
      if (e.button !== 0 || (e.target as Element).closest('input, textarea, [data-no-swipe]')) return;
      start = { x: e.clientX, y: e.clientY, t: e.timeStamp, id: e.pointerId };
      leading = false;
      dx = 0;
      last = { x: e.clientX, t: e.timeStamp };
      speed = 0;
    };
    const onMove = (e: PointerEvent) => {
      if (!start || e.pointerId !== start.id) return;
      const mx = e.clientX - start.x;
      const my = e.clientY - start.y;
      if (!leading) {
        if (Math.abs(my) > SLOP_PX && Math.abs(my) > Math.abs(mx)) {
          start = null;
          return;
        }
        if (Math.abs(mx) < SLOP_PX) return;
        leading = true;
        el.setPointerCapture(e.pointerId);
      }
      const dt = Math.max(1, e.timeStamp - last.t);
      speed = softSpeed(speed, (e.clientX - last.x) / dt);
      last = { x: e.clientX, t: e.timeStamp };
      dx = mx;
      set(dx);
    };
    const onUp = (e: PointerEvent) => {
      if (!start || e.pointerId !== start.id) return;
      start = null;
      if (!leading) return;
      leading = false;
      swallow = true;
      window.setTimeout(() => {
        swallow = false;
      }, 0);
      settle(dx, releaseSpeed(speed, last.t, e.timeStamp));
    };
    // A drag that moved the row is not a click on it.
    const onClick = (e: MouseEvent) => {
      if (swallow) {
        e.stopPropagation();
        e.preventDefault();
      }
    };
    // Two fingers sideways on a trackpad.
    const onWheel = (e: WheelEvent) => {
      if (Math.abs(e.deltaX) <= Math.abs(e.deltaY)) return;
      e.preventDefault();
      wheel -= e.deltaX;
      set(wheel);
      window.clearTimeout(wheelTimer);
      wheelTimer = window.setTimeout(() => {
        const shift = wheel;
        wheel = 0;
        settle(shift, 0);
      }, WHEEL_IDLE_MS);
    };

    el.addEventListener('pointerdown', onDown);
    el.addEventListener('pointermove', onMove);
    el.addEventListener('pointerup', onUp);
    el.addEventListener('pointercancel', onUp);
    el.addEventListener('click', onClick, true);
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => {
      window.clearTimeout(wheelTimer);
      if (frame !== 0) cancelAnimationFrame(frame);
      el.removeEventListener('pointerdown', onDown);
      el.removeEventListener('pointermove', onMove);
      el.removeEventListener('pointerup', onUp);
      el.removeEventListener('pointercancel', onUp);
      el.removeEventListener('click', onClick, true);
      el.removeEventListener('wheel', onWheel);
    };
  }, [target, enabled]);
};
