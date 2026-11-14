// One budget for every frame the page keeps alive out of sight: the screens of the last tabs (KeptScreens in
// bindings/shell.tsx) and the pane's views and browser tabs (features/app-views/kept-frames.tsx). Each owner tells what
// it keeps, which of it is on screen and when the rest was last seen; the ones out of sight beyond the device's count
// go, the least recent first, whoever owns them. What is on screen is never let go.
import { keptScreensMax } from '@/next/features/shell/kept-screens';

export interface IKeptEntry {
  key: string;
  shown: boolean;
  /** When it was last on screen (ms). */
  at: number;
}

/** Out-of-sight frames the page keeps: the device's count of kept screens less the one on screen. */
export const keptHiddenMax = (deviceMemory: number | undefined): number => keptScreensMax(deviceMemory) - 1;

/** The keys over the budget: the hidden ones beyond `max`, the least recent first to go. */
export const overBudget = (entries: readonly IKeptEntry[], max: number): ReadonlySet<string> => {
  const hidden = entries.filter((e) => !e.shown).sort((a, b) => b.at - a.at);
  return new Set(hidden.slice(Math.max(0, max)).map((e) => e.key));
};

const sameSet = (a: ReadonlySet<string>, b: ReadonlySet<string>) =>
  a.size === b.size && [...a].every((k) => b.has(k));

export const makeKeptBudget = (max: number) => {
  const owners = new Map<string, readonly IKeptEntry[]>();
  let over: ReadonlySet<string> = new Set();
  const listeners = new Set<() => void>();
  return {
    /** The owner keeps these now (all of them: what it no longer reports is no longer counted). */
    report(owner: string, entries: readonly IKeptEntry[]) {
      owners.set(owner, entries);
      const next = overBudget([...owners.values()].flat(), max);
      if (sameSet(next, over)) return;
      over = next;
      for (const listener of [...listeners]) listener();
    },
    /** May it stay: false once it went over the budget. */
    allows: (key: string) => !over.has(key),
    /** The keys over the budget, the same set until it changes (for useSyncExternalStore). */
    over: () => over,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
};

export const keptBudget = makeKeptBudget(
  keptHiddenMax(
    typeof navigator === 'undefined'
      ? undefined
      : (navigator as Navigator & { deviceMemory?: number }).deviceMemory,
  ),
);
