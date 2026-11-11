import { useLayoutEffect, useSyncExternalStore } from 'react';

/**
 * The desktop sidebar folded to a rail. One rule: only the person opens it. Moving between tabs and screens, the pane
 * shutting or a design left never open it by themselves; the window only folds it when there is no room.
 *
 * The person's choice is kept per device, apart for two kinds of screens: the ordinary ones (the chats, the start page,
 * Settings) and the full-screen ones (an app, a design, a document of an app's type, a kit), where the sidebar starts
 * folded until the person opens it there.
 */
export const RAIL_MODE = { screen: 'screen', full: 'full' } as const;
export type TRailMode = (typeof RAIL_MODE)[keyof typeof RAIL_MODE];

const RAIL_KEY = {
  screen: 'hx-sidebar-collapsed',
  full: 'hx-sidebar-collapsed-full',
} as const satisfies Record<TRailMode, string>;
/** Where nothing was chosen yet: open on the ordinary screens, folded on the full-screen ones. */
const RAIL_DEFAULT = { screen: false, full: true } as const satisfies Record<TRailMode, boolean>;

const listeners = new Set<() => void>();
const folded: Record<TRailMode, boolean | null> = { screen: null, full: null };
let mode: TRailMode = RAIL_MODE.screen;

const read = (m: TRailMode): boolean => {
  try {
    const kept = localStorage.getItem(RAIL_KEY[m]);
    return kept === null ? RAIL_DEFAULT[m] : kept === '1';
  } catch {
    return RAIL_DEFAULT[m];
  }
};

const write = (m: TRailMode, value: boolean) => {
  try {
    localStorage.setItem(RAIL_KEY[m], value ? '1' : '0');
  } catch {
    // Private mode or full storage: the rail still folds for this page.
  }
};

/** What the person chose for the screens now on, kept on the device. */
const stored = (): boolean => (folded[mode] ??= read(mode));

/**
 * The sidebar folded for the right pane on a narrow window: drawn folded, never kept. It stays folded until the person
 * opens it: the pane shutting, or its chat left for another tab, does not open it again.
 */
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
 * Folds the sidebar to its rail and opens it again, the person's own act. The state lives here and not in the shell:
 * the toggle then re-renders only what draws the rail, not the whole shell with the chat in it.
 */
export const toggleRail = () => {
  const next = !snapshot();
  foldedForPane = false;
  folded[mode] = next;
  write(mode, next);
  notify();
};

/** Which screens are on: the shell sets it from the route, before the frame is painted. */
export const setRailMode = (next: TRailMode) => {
  if (mode === next) return;
  mode = next;
  notify();
};

/** Sets the rail's mode from the shell's route, before paint, so a full-screen screen never shows the sidebar a frame. */
export const useRailMode = (next: TRailMode) => {
  useLayoutEffect(() => setRailMode(next), [next]);
};

/** Narrower than this, the open sidebar, the chat (with a design beside it) and the right pane do not fit side by side. */
export const PANE_FOLD_MAX_W = 1500;

/**
 * The right pane opens on a narrow window: the open sidebar folds to its rail, with the same motion as the person's
 * fold, and what the person chose stays kept as it was. Not on a phone: there the sidebar is a drawer.
 */
export const foldRailForPane = (windowWidth: number) => {
  if (snapshot() || windowWidth >= PANE_FOLD_MAX_W) return;
  foldedForPane = true;
  notify();
};

export const useRail = (): boolean => useSyncExternalStore(subscribe, snapshot, () => false);

/** Whether a full-screen screen is on (an app, a design). */
export const useRailFull = (): boolean =>
  useSyncExternalStore(
    subscribe,
    () => mode === RAIL_MODE.full,
    () => false,
  );
