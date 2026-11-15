// «Правка на глазах»: while the voice makes a quick edit of a board, the block it is about wears a ring on
// the board on screen — dashed when found, solid with a sweep while it changes, a flash when done. The voice says the
// phase here; the site view tells its board (`tk:live`).
import { useSyncExternalStore } from 'react';

export const LIVE_PHASE = { found: 'found', editing: 'editing', done: 'done' } as const;
export type TLivePhase = (typeof LIVE_PHASE)[keyof typeof LIVE_PHASE];

export interface ILiveMark {
  /** The block: the board's template number. */
  tpl: number;
  phase: TLivePhase;
}

let mark: ILiveMark | null = null;
const listeners = new Set<() => void>();

export const setLiveMark = (next: ILiveMark | null) => {
  mark = next;
  for (const fn of listeners) fn();
};

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => {
    listeners.delete(fn);
  };
};

export const useLiveMark = (): ILiveMark | null =>
  useSyncExternalStore(
    subscribe,
    () => mark,
    () => null,
  );

/** The changed lines of an edit without a picked block: the board finds the block they make and flashes it. */
export interface ILiveHint {
  hint: string;
  /** A new one each time, so the same lines changed twice flash twice. */
  n: number;
}

let hint: ILiveHint | null = null;
let hints = 0;
const hintListeners = new Set<() => void>();

export const flashLiveHint = (text: string) => {
  hints += 1;
  hint = { hint: text, n: hints };
  for (const fn of hintListeners) fn();
};

const subscribeHint = (fn: () => void) => {
  hintListeners.add(fn);
  return () => {
    hintListeners.delete(fn);
  };
};

export const useLiveHint = (): ILiveHint | null =>
  useSyncExternalStore(
    subscribeHint,
    () => hint,
    () => null,
  );
