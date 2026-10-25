// Leaving with an animation: what closes stays mounted in a `closing` phase for its exit (spec §6, «every closing
// element has a closing phase»), and keeps showing the last thing it showed while it leaves.
import { useEffect, useState } from 'react';

export const PRESENCE = { open: 'open', closing: 'closing' } as const;
export type TPresenceState = (typeof PRESENCE)[keyof typeof PRESENCE];

export interface IPresence<T> {
  /** What to draw: the current value, or the last one while it leaves; null once it is gone. */
  value: T | null;
  state: TPresenceState;
}

/** Keeps `value` on screen for `exitMs` after it turns null, in the closing phase. */
export const usePresence = <T>(value: T | null, exitMs: number): IPresence<T> => {
  const [shown, setShown] = useState<T | null>(value);
  const [closing, setClosing] = useState(false);
  // A new value replaces the shown one at once; null starts the exit (state adjusted during render, not in an effect).
  if (value !== null && (shown !== value || closing)) {
    setShown(value);
    setClosing(false);
  }
  if (value === null && shown !== null && !closing) setClosing(true);

  useEffect(() => {
    if (!closing) return;
    const timer = setTimeout(() => {
      setShown(null);
      setClosing(false);
    }, exitMs);
    return () => clearTimeout(timer);
  }, [closing, exitMs]);

  return { value: value ?? shown, state: closing ? PRESENCE.closing : PRESENCE.open };
};

/** The same for a flag: mounted while open and through its exit. */
export const usePresentFlag = (
  open: boolean,
  exitMs: number,
): { mounted: boolean; state: TPresenceState } => {
  const { value, state } = usePresence(open ? true : null, exitMs);
  return { mounted: value !== null, state };
};

/** True from the first time `on` is true: a folding body keeps its content after it opened once, so it can fold back
 *  with an animation instead of vanishing. */
export const useSeen = (on: boolean): boolean => {
  const [seen, setSeen] = useState(on);
  if (on && !seen) setSeen(true);
  return seen || on;
};

/** A list whose removed items stay `exitMs` in a leaving phase, in their old place, so they can animate out. */
export const useLeavingList = <T>(
  items: readonly T[],
  keyOf: (item: T) => string,
  exitMs: number,
): { item: T; leaving: boolean }[] => {
  const [gone, setGone] = useState<{ item: T; index: number }[]>([]);
  const [last, setLast] = useState(items);
  if (last !== items) {
    const kept = new Set(items.map(keyOf));
    const removed = last.map((item, index) => ({ item, index })).filter(({ item }) => !kept.has(keyOf(item)));
    setLast(items);
    if (removed.length > 0)
      setGone((current) => [...current.filter(({ item }) => !kept.has(keyOf(item))), ...removed]);
  }
  useEffect(() => {
    if (gone.length === 0) return;
    const timer = setTimeout(() => setGone([]), exitMs);
    return () => clearTimeout(timer);
  }, [gone, exitMs]);
  const out = items.map((item) => ({ item, leaving: false }));
  [...gone]
    .sort((a, b) => a.index - b.index)
    .forEach(({ item, index }) => out.splice(Math.min(index, out.length), 0, { item, leaving: true }));
  return out;
};
