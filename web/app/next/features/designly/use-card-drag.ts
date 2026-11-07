import { useEffect } from 'react';
import { useStableFn } from '@/next/ui/use-stable-fn';

// A drag of library cards by hand (no drag-and-drop library): a mouse picks a card up after it moves a few pixels, a
// finger after it rests (a swipe is the page's scroll). A ghost follows the pointer; what it is over lights up
// (`data-drop-over` on the nearest `[data-drop-folder]`, whose value is the folder id, '' for the top). Letting go there
// hands the owner the ids; Escape or a drop nowhere puts it back. Nothing renders while it goes: the ghost's place is a
// custom property written once a frame.

const MOUSE_SLOP = 5;
const TOUCH_SLOP = 8;
const HOLD_MS = 420;
const DROP_ATTR = 'data-drop-folder';

export interface ICardDragOptions {
  /** The cards a pick-up of `id` carries: the selection when `id` is in it, else `id` alone. */
  carry: (id: string) => string[];
  /** Whether the card may be picked up at all. */
  enabled: boolean;
  onDrop: (ids: string[], folder: string) => void;
  /** Called when a drag starts, so the owner can drop a click that follows it. */
  onStart?: () => void;
}

const dropAt = (x: number, y: number): HTMLElement | null =>
  document.elementFromPoint(x, y)?.closest<HTMLElement>(`[${DROP_ATTR}]`) ?? null;

/** Wires the grid element: cards say what they are by `data-id`, places by `data-drop-folder`. */
export const useCardDrag = (grid: HTMLElement | null, options: ICardDragOptions): void => {
  const carry = useStableFn(options.carry);
  const onDrop = useStableFn(options.onDrop);
  const onStart = useStableFn(options.onStart ?? (() => undefined));
  const { enabled } = options;

  useEffect(() => {
    if (!grid) return;
    let timer = 0;
    let frame = 0;
    let ghost: HTMLElement | null = null;
    let over: HTMLElement | null = null;
    let ids: string[] = [];
    let pending: { id: string; x: number; y: number; touch: boolean; pointer: number } | null = null;
    let active = false;
    let last = { x: 0, y: 0 };

    const mark = (target: HTMLElement | null) => {
      if (target === over) return;
      over?.removeAttribute('data-drop-over');
      over = target;
      over?.setAttribute('data-drop-over', 'true');
    };
    const place = () => {
      frame = 0;
      if (!ghost) return;
      ghost.style.setProperty('--x', `${last.x}px`);
      ghost.style.setProperty('--y', `${last.y}px`);
      mark(dropAt(last.x, last.y));
    };
    const cleanup = () => {
      window.clearTimeout(timer);
      cancelAnimationFrame(frame);
      frame = 0;
      mark(null);
      ghost?.remove();
      ghost = null;
      pending = null;
      active = false;
      document.documentElement.removeAttribute('data-card-dragging');
      for (const el of grid.querySelectorAll('[data-lifted]')) el.removeAttribute('data-lifted');
    };
    const start = () => {
      if (!pending) return;
      active = true;
      ids = carry(pending.id);
      onStart();
      ghost = document.createElement('div');
      ghost.className = 'hx-dl-ghost';
      ghost.textContent = String(ids.length);
      ghost.dataset.many = String(ids.length > 1);
      const first = grid.querySelector<HTMLElement>(
        `[data-id="${CSS.escape(pending.id)}"] .hx-dl-card__title`,
      );
      if (first && ids.length === 1) ghost.textContent = first.textContent;
      document.body.append(ghost);
      document.documentElement.setAttribute('data-card-dragging', 'true');
      for (const id of ids)
        grid.querySelector(`[data-id="${CSS.escape(id)}"]`)?.setAttribute('data-lifted', 'true');
      if (navigator.vibrate) navigator.vibrate(8);
      last = { x: pending.x, y: pending.y };
      place();
    };
    const move = (event: PointerEvent) => {
      if (pending && !active) {
        if (event.pointerId !== pending.pointer) return;
        const far = Math.hypot(event.clientX - pending.x, event.clientY - pending.y);
        if (!pending.touch && far > MOUSE_SLOP) {
          pending.x = event.clientX;
          pending.y = event.clientY;
          start();
        } else if (pending.touch && far > TOUCH_SLOP) cleanup();
        return;
      }
      if (!active) return;
      event.preventDefault();
      last = { x: event.clientX, y: event.clientY };
      if (!frame) frame = requestAnimationFrame(place);
    };
    const end = (event: PointerEvent) => {
      if (!pending && !active) return;
      const target = active ? dropAt(event.clientX, event.clientY) : null;
      const carried = ids;
      const was = active;
      cleanup();
      if (was && target && event.type === 'pointerup') onDrop(carried, target.getAttribute(DROP_ATTR) ?? '');
    };
    const down = (event: PointerEvent) => {
      if (!enabled || (event.pointerType === 'mouse' && event.button !== 0)) return;
      const target = event.target;
      if (!(target instanceof Element) || target.closest('button, a, input, [data-no-drag]')) return;
      const card = target.closest<HTMLElement>('[data-id]');
      if (!card || !grid.contains(card) || !card.dataset.id) return;
      cleanup();
      const touch = event.pointerType !== 'mouse';
      pending = { id: card.dataset.id, x: event.clientX, y: event.clientY, touch, pointer: event.pointerId };
      if (touch) timer = window.setTimeout(start, HOLD_MS);
    };
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && (active || pending)) cleanup();
    };
    // A click that ends a drag is not an open.
    const swallow = (event: MouseEvent) => {
      if (swallowNext) {
        swallowNext = false;
        event.stopPropagation();
        event.preventDefault();
      }
    };
    let swallowNext = false;
    const endAndSwallow = (event: PointerEvent) => {
      if (active) {
        swallowNext = true;
        window.setTimeout(() => {
          swallowNext = false;
        }, 80);
      }
      end(event);
    };
    // A held finger must not scroll the page once the card is up.
    const noScroll = (event: TouchEvent) => {
      if (active) event.preventDefault();
    };
    const noMenu = (event: Event) => {
      if (active || pending?.touch) event.preventDefault();
    };

    grid.addEventListener('pointerdown', down);
    grid.addEventListener('click', swallow, true);
    grid.addEventListener('contextmenu', noMenu);
    window.addEventListener('pointermove', move, { passive: false });
    window.addEventListener('pointerup', endAndSwallow);
    window.addEventListener('pointercancel', endAndSwallow);
    window.addEventListener('keydown', key);
    window.addEventListener('touchmove', noScroll, { passive: false });
    return () => {
      cleanup();
      grid.removeEventListener('pointerdown', down);
      grid.removeEventListener('click', swallow, true);
      grid.removeEventListener('contextmenu', noMenu);
      window.removeEventListener('pointermove', move);
      window.removeEventListener('pointerup', endAndSwallow);
      window.removeEventListener('pointercancel', endAndSwallow);
      window.removeEventListener('keydown', key);
      window.removeEventListener('touchmove', noScroll);
    };
  }, [grid, enabled, carry, onDrop, onStart]);
};
