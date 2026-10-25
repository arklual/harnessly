// The edit card of a picked block is moved by its head, with a mouse or a finger, so the person can see what
// it covered. It stays 8 px inside the view (its CSS clamps it there again when the view shrinks); where it was put is
// kept for the browser's session, and the next card opens there. A double press on the head brings it back to its block.
import type { PointerEvent as ReactPointerEvent } from 'react';
import { tweaksMovedBy, type IPoint } from '@/next/features/designly/site-drag';

const KEY = 'hx-note-pos';
/** A press that moved less than this is a press, not a drag. */
const SLOP = 3;

export const parseNotePos = (raw: string | null): IPoint | null => {
  if (!raw) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (typeof value !== 'object' || value === null) return null;
    const { x, y } = value as Record<string, unknown>;
    return typeof x === 'number' && typeof y === 'number' && Number.isFinite(x) && Number.isFinite(y)
      ? { x, y }
      : null;
  } catch {
    return null;
  }
};

/** Where the person put the card in this session; null while it opens at its block. */
export const readNotePos = (): IPoint | null => {
  try {
    return parseNotePos(window.sessionStorage.getItem(KEY));
  } catch {
    return null;
  }
};

export const keepNotePos = (at: IPoint | null) => {
  try {
    if (at) window.sessionStorage.setItem(KEY, JSON.stringify(at));
    else window.sessionStorage.removeItem(KEY);
  } catch {
    // A private window or blocked storage: the card is moved for this time only.
  }
};

/** The drag of the card by its head: the corner goes to --mx/--my once a frame, `onMoved` gets it on release. */
export const dragNote = (event: ReactPointerEvent<HTMLElement>, onMoved: (at: IPoint) => void) => {
  const card = event.currentTarget.closest<HTMLElement>('.hx-note');
  const home = card?.offsetParent;
  if (!card || !(home instanceof HTMLElement)) return;
  if (event.pointerType === 'mouse' && event.button !== 0) return;
  if ((event.target as Element).closest('button, textarea, input')) return;
  event.preventDefault();
  const h = home.getBoundingClientRect();
  const c = card.getBoundingClientRect();
  const box = { x: c.left - h.left, y: c.top - h.top, w: c.width, h: c.height, bw: h.width, bh: h.height };
  const from = { x: event.clientX, y: event.clientY };
  const id = event.pointerId;
  let last = from;
  let cur: IPoint | null = null;
  let frame = 0;
  const step = () => {
    frame = 0;
    cur = tweaksMovedBy(box, last.x - from.x, last.y - from.y);
    card.dataset.moved = 'true';
    card.dataset.dragging = 'true';
    card.style.setProperty('--mx', `${cur.x}px`);
    card.style.setProperty('--my', `${cur.y}px`);
  };
  const onMove = (e: PointerEvent) => {
    if (e.pointerId !== id) return;
    last = { x: e.clientX, y: e.clientY };
    if (!cur && Math.abs(last.x - from.x) + Math.abs(last.y - from.y) <= SLOP) return;
    if (frame === 0) frame = requestAnimationFrame(step);
  };
  const onEnd = (e: PointerEvent) => {
    if (e.pointerId !== id) return;
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onEnd);
    window.removeEventListener('pointercancel', onEnd);
    if (frame !== 0) {
      cancelAnimationFrame(frame);
      step();
    }
    card.dataset.dragging = 'false';
    if (cur) onMoved(cur);
  };
  window.addEventListener('pointermove', onMove);
  window.addEventListener('pointerup', onEnd);
  window.addEventListener('pointercancel', onEnd);
};
