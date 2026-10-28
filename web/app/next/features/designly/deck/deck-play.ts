// Moving through the talk in the show and the recording: the keys and the rule «a step of the slide first, then the next
// slide». The shell owns the slide frames, so it passes in where the talk stands and how to move it (IDeckPlayHost).
import { useEffect, useEffectEvent } from 'react';
import type { IDeckSlide } from '@/next/features/designly/deck/types';

/** A pointer position over the slide, in the board's pixels. */
export interface IDeckPointer {
  x: number;
  y: number;
  down: boolean;
}

/** What the shell gives the show and the recording. */
export interface IDeckPlayHost {
  slides: readonly IDeckSlide[];
  /** The slide on screen, 0-based. */
  index: number;
  onGo: (index: number) => void;
  /** Steps of the slide on screen (`tk:deck-ready`) and the one shown now; step 0 shows none of them. */
  steps: number;
  step: number;
  onStep: (step: number) => void;
  /** The slide's box on screen (the frame's rectangle), for the laser and the cursor. Null while no frame is up. */
  slideRect: () => DOMRect | null;
  /** The board's pointer messages (`tk:deck-pointer`): returns the way to stop listening. */
  onPointer: (listener: (pointer: IDeckPointer) => void) => () => void;
  /** Asks the board to report the pointer (or to stop). */
  setPointer: (on: boolean) => void;
  /** The plain address of a board for a small copy of it (the next slide's mini); null before the slot is open. */
  frameSrc: (path: string) => string | null;
}

export type TPlayMove = { to: 'step'; step: number } | { to: 'slide'; index: number } | null;

/** Forward: the next step of the slide, then the next slide; null at the very end. */
export const nextMove = (index: number, total: number, step: number, steps: number): TPlayMove => {
  if (step < steps) return { to: 'step', step: step + 1 };
  return index < total - 1 ? { to: 'slide', index: index + 1 } : null;
};

/** Back: the previous step, then the previous slide; null at the very start. */
export const prevMove = (index: number, step: number): TPlayMove => {
  if (step > 0) return { to: 'step', step: step - 1 };
  return index > 0 ? { to: 'slide', index: index - 1 } : null;
};

export const PLAY_KEY = { next: 'next', prev: 'prev', exit: 'exit', record: 'record' } as const;
export type TPlayKey = (typeof PLAY_KEY)[keyof typeof PLAY_KEY];

/** What a key press means in the show; null: not ours. Modifier chords are left to the browser. */
export const playKey = (e: {
  key: string;
  ctrlKey?: boolean;
  metaKey?: boolean;
  altKey?: boolean;
}): TPlayKey | null => {
  if (e.ctrlKey || e.metaKey || e.altKey) return null;
  switch (e.key) {
    case 'ArrowRight':
    case 'ArrowDown':
    case 'PageDown':
    case ' ':
      return PLAY_KEY.next;
    case 'ArrowLeft':
    case 'ArrowUp':
    case 'PageUp':
      return PLAY_KEY.prev;
    case 'Escape':
      return PLAY_KEY.exit;
    case 'r':
    case 'R':
    case 'к':
    case 'К':
      return PLAY_KEY.record;
    default:
      return null;
  }
};

const typing = (target: EventTarget | null) =>
  target instanceof HTMLElement &&
  (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName));

export interface IPlayHandlers {
  onExit: () => void;
  /** Absent: the R key does nothing (in the recording itself). */
  onRecord?: () => void;
}

/** Arrows, Space and the rest, for as long as the show or the recording is on screen. Returns the two moves. */
export const useDeckPlay = (host: IDeckPlayHost, handlers: IPlayHandlers) => {
  const total = host.slides.length;
  const move = (m: TPlayMove) => {
    if (!m) return;
    if (m.to === 'step') {
      host.onStep(m.step);
      return;
    }
    host.onStep(0);
    host.onGo(m.index);
  };
  const next = () => move(nextMove(host.index, total, host.step, host.steps));
  const prev = () => move(prevMove(host.index, host.step));

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    if (typing(e.target)) return;
    const key = playKey(e);
    if (!key) return;
    e.preventDefault();
    if (key === PLAY_KEY.next) next();
    else if (key === PLAY_KEY.prev) prev();
    else if (key === PLAY_KEY.exit) handlers.onExit();
    else handlers.onRecord?.();
  });
  useEffect(() => {
    const listener = (e: KeyboardEvent) => onKey(e);
    window.addEventListener('keydown', listener);
    return () => window.removeEventListener('keydown', listener);
  }, []);

  return { next, prev };
};
