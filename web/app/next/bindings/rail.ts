import { useSyncExternalStore } from 'react';

/** The desktop sidebar folded to a rail; per device. */
const RAIL_KEY = 'hx-sidebar-collapsed';

const listeners = new Set<() => void>();
let folded: boolean | null = null;

const read = (): boolean => {
  try {
    return localStorage.getItem(RAIL_KEY) === '1';
  } catch {
    return false;
  }
};

const write = (value: boolean) => {
  try {
    localStorage.setItem(RAIL_KEY, value ? '1' : '0');
  } catch {
    // Private mode or full storage: the rail still folds for this page.
  }
};

/** What the person chose, kept on the device. */
const stored = (): boolean => (folded ??= read());

/** The sidebar folded for the right pane on a narrow window: drawn folded, never kept (HRN-674). */
let foldedForPane = false;

const snapshot = (): boolean => stored() || foldedForPane;

const notify = () => {
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/**
 * Folds the sidebar to its rail and opens it again. The state lives here and not in the shell: the toggle then
 * re-renders only what draws the rail, not the whole shell with the chat in it, so the click's frame stays short.
 */
const set = (value: boolean) => {
  folded = value;
  write(folded);
  notify();
};

/** The sidebar was folded for a design opened full screen, and opens again when the person leaves it. */
let foldedForDesign = false;

export const toggleRail = () => {
  const next = !snapshot();
  foldedForDesign = false;
  foldedForPane = false;
  set(next);
};

/**
 * A design opened full screen from the right pane takes the whole window: the open sidebar folds to its rail, and the
 * person can still open it there (HRN-673).
 */
export const foldRailForDesign = () => {
  if (stored()) return;
  foldedForDesign = true;
  set(true);
};

/** Out of the design: the sidebar folded for it opens again, unless the person changed it meanwhile. */
export const restoreRailAfterDesign = () => {
  if (!foldedForDesign) return;
  foldedForDesign = false;
  set(false);
};

/** Narrower than this, the open sidebar, the chat (with a design beside it) and the right pane do not fit side by side. */
export const PANE_FOLD_MAX_W = 1500;

/**
 * The right pane opens on a narrow window: the open sidebar folds to its rail, with the same motion as the person's
 * fold, and what the person chose stays kept as it was (HRN-674). Not on a phone: there the sidebar is a drawer.
 */
export const foldRailForPane = (windowWidth: number) => {
  if (snapshot() || windowWidth >= PANE_FOLD_MAX_W) return;
  foldedForPane = true;
  notify();
};

/** The pane shut: the sidebar folded for it opens again, unless the person folded or opened it meanwhile. */
export const restoreRailAfterPane = () => {
  if (!foldedForPane) return;
  foldedForPane = false;
  notify();
};

export const useRail = (): boolean => useSyncExternalStore(subscribe, snapshot, () => false);
