import { paneTabId, type TPaneTabKind } from '@/lib/hl-pane-tabs';

/**
 * Tabs the agent opened by itself (`hl open`, an artifact, a board, a dev server), waiting for their panel
 * to play their arrival once. A panel that was hidden mounts with the tab already in its state, so a diff of its tabs
 * would not see it as new: the mark carries the news across the mount.
 */
const pending = new Map<string, Set<string>>();
const EMPTY: ReadonlySet<string> = new Set();

export const markArrival = (paneKey: string, kind: TPaneTabKind, target: string | null): void => {
  const ids = pending.get(paneKey) ?? new Set<string>();
  ids.add(paneTabId(kind, target));
  pending.set(paneKey, ids);
};

/** The panel is about to mount for a tab the agent brought: its contents wait for the slide to end. */
export const hasArrivals = (paneKey: string): boolean => (pending.get(paneKey)?.size ?? 0) > 0;

/** How long a panel that slid in for an arrival keeps its contents back: the length of the column's movement. */
export const ARRIVAL_SETTLE_MS = 560;
/** The longest the skeleton waits over contents that have not filled in yet. */
export const ARRIVAL_REVEAL_MAX_MS = 1600;

export const takeArrivals = (paneKey: string): ReadonlySet<string> => {
  const ids = pending.get(paneKey);
  if (!ids) return EMPTY;
  pending.delete(paneKey);
  return ids;
};

/** A spring that settles with a 1% overshoot: stiffness 260, damping 26, sampled for CSS `linear()` and WAAPI. */
export const SPRING =
  'linear(0, 0.02662, 0.09177, 0.1798, 0.279, 0.3809, 0.4797, 0.5717, 0.6546, 0.725, 0.7896, 0.8403, 0.8848, 0.9194, 0.9467, 0.9677, 0.9834, 0.9949, 1.003, 1.008, 1.011, 1.013, 1.013, 1.013, 1.012, 1.011, 1.01, 1.008, 1.007, 1.006, 1.005, 1.004, 1.003, 1.002, 1.002, 1.001, 1.001, 1, 1, 1, 1)';
export const SPRING_MS = 560;

/** The tabs a panel shows that it did not show at its last look, plus those the agent marked while it was away. */
export const bornTabs = (
  ids: readonly string[],
  before: ReadonlySet<string> | undefined,
  marked: ReadonlySet<string>,
): string[] => ids.filter((id) => marked.has(id) || (before !== undefined && !before.has(id)));
