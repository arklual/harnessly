// The review screen's view settings this browser remembers: two columns or one, and whether whitespace counts.
import { useSyncExternalStore } from 'react';
import { DIFF_LAYOUT, type TDiffLayout } from '@/next/features/pane/editor/diff-view';

export interface IReviewPrefs {
  layout: TDiffLayout;
  ignoreWhitespace: boolean;
}

const KEY = 'hx.review.prefs';
export const DEFAULT_PREFS: IReviewPrefs = { layout: DIFF_LAYOUT.split, ignoreWhitespace: false };

export const parsePrefs = (raw: string | null): IReviewPrefs => {
  if (!raw) return DEFAULT_PREFS;
  try {
    const value = JSON.parse(raw) as Partial<Record<keyof IReviewPrefs, unknown>>;
    return {
      layout: value.layout === DIFF_LAYOUT.inline ? DIFF_LAYOUT.inline : DIFF_LAYOUT.split,
      ignoreWhitespace: value.ignoreWhitespace === true,
    };
  } catch {
    return DEFAULT_PREFS;
  }
};

let current: IReviewPrefs | null = null;
const listeners = new Set<() => void>();

const read = (): IReviewPrefs => {
  if (current) return current;
  try {
    current = parsePrefs(window.localStorage.getItem(KEY));
  } catch {
    current = DEFAULT_PREFS;
  }
  return current;
};

export const setReviewPrefs = (patch: Partial<IReviewPrefs>) => {
  current = { ...read(), ...patch };
  try {
    window.localStorage.setItem(KEY, JSON.stringify(current));
  } catch {
    // Private mode or a full storage: the setting holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const useReviewPrefs = (): IReviewPrefs => useSyncExternalStore(subscribe, read, () => DEFAULT_PREFS);
