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
      d.item.until === b[i]?.item.until &&
      d.what === b[i]?.what,
  );

/** The binding of the smart notice says what is in the drops now. */
export const setDrops = (next: readonly IDrop[]) => {
  if (same(drops, next)) return;
  drops = next.length === 0 ? NONE : next;
  emit();
};

export const useDrops = (): readonly IDrop[] =>
  useSyncExternalStore(
    subscribe,
    () => drops,
    () => NONE,
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
