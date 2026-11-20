// The drops: what waits for the person and was put away from the smart notice (a code, a permission, a question, a
// machine in the red) does not go: it stays as a small drop in the chat's right column until it is answered, and is
// the only reminder of it (the notice does not bring it back by itself). The smart notice's binding says what is in
// the drops; the column draws them; a drop pressed asks the notice to take it back (and plays the flight from the drop
// into the card). A page's state, not saved: a reload brings what still waits back to the notice.
import { useSyncExternalStore } from 'react';
import type { ISmartItem } from '@/next/features/smart-notice/smart-notice';

export interface IDrop {
  item: ISmartItem;
  /** What it asks, in a few words more than its line: a code's reason, a question's text. */
  what: string | null;
}

/** A drop asked back: where it stood on screen, so the card can grow out of it. */
export interface IDropBack {
  key: string;
  from: DOMRect | null;
  view: HTMLElement | null;
  at: number;
}

const NONE: readonly IDrop[] = [];
let drops: readonly IDrop[] = NONE;
/** Swept off the stack or «Очистить всё»: not shown while it still waits (the notification centre keeps its row). */
let dismissed: ReadonlySet<string> = new Set();
/** What the column shows: the drops less the ones taken off the stack; the same list while nothing changed. */
let visible: readonly IDrop[] = NONE;
const refresh = () => {
  const next = drops.filter((d) => !dismissed.has(d.item.key));
  if (next.length === drops.length) visible = drops;
  else visible = next.length === 0 ? NONE : next;
};
let back: IDropBack | null = null;
const listeners = new Set<() => void>();
const emit = () => {
  for (const listener of listeners) listener();
};
const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

const same = (a: readonly IDrop[], b: readonly IDrop[]) =>
  a.length === b.length &&
  a.every(
    (d, i) =>
      d.item.key === b[i]?.item.key &&
      d.item.title === b[i]?.item.title &&
      d.item.text === b[i]?.item.text &&
      d.item.summary === b[i]?.item.summary &&
      d.item.until === b[i]?.item.until &&
      d.what === b[i]?.what,
  );

/** The binding of the smart notice says what is in the drops now. */
export const setDrops = (next: readonly IDrop[]) => {
  if (same(drops, next)) return;
  drops = next.length === 0 ? NONE : next;
  // What is no longer waiting forgets it was taken off: if it waits again, it comes back.
  const keys = new Set(drops.map((d) => d.item.key));
  if ([...dismissed].some((k) => !keys.has(k)))
    dismissed = new Set([...dismissed].filter((k) => keys.has(k)));
  refresh();
  emit();
};

/** Takes drops off the stack (a swipe, «Убрать», «Очистить всё»). */
export const dismissDrops = (keys: readonly string[]) => {
  if (keys.every((k) => dismissed.has(k))) return;
  dismissed = new Set([...dismissed, ...keys]);
  refresh();
  emit();
};

export const useDrops = (): readonly IDrop[] =>
  useSyncExternalStore(
    subscribe,
    () => visible,
    () => NONE,
  );

/** Drops the notice is flying into: drawn hidden from their first frame until the flight lands, so they never show
 *  their own entrance before it, nor play it again after it. */
let arriving: ReadonlySet<string> = new Set();
/** The longest a flight into a drop may take before the drop shows anyway, ms. */
const ARRIVE_MAX_MS = 2000;

/** The notice is put away into this drop by hand: the flight draws it until it lands. */
export const markArriving = (key: string) => {
  if (arriving.has(key)) return;
  arriving = new Set([...arriving, key]);
  emit();
  // A flight that never ends (the tab hidden, the drop gone) does not leave the drop hidden.
  setTimeout(() => landDrop(key), ARRIVE_MAX_MS);
};

/** The flight into the drop has landed (or found no drop to land in). */
export const landDrop = (key: string) => {
  if (!arriving.has(key)) return;
  const next = new Set(arriving);
  next.delete(key);
  arriving = next;
  emit();
};

/** True while the flight into this drop is on. */
export const useDropArriving = (key: string): boolean =>
  useSyncExternalStore(
    subscribe,
    () => arriving.has(key),
    () => false,
  );

/** A drop pressed: the notice takes it back, open. */
export const askDropBack = (key: string, from: DOMRect | null, view: HTMLElement | null) => {
  back = { key, from, view, at: Date.now() };
  emit();
};

export const useDropBack = (): IDropBack | null =>
  useSyncExternalStore(
    subscribe,
    () => back,
    () => null,
  );

/** The notice has taken it. */
export const takeDropBack = () => {
  if (back === null) return;
  back = null;
  emit();
};
