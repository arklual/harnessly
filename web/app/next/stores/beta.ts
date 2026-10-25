// «Бета» (Settings → Beta): features that are not ready for everyone, off until the person turns them on. This
// browser's, as the theme and the feed's cards are.
import { useSyncExternalStore } from 'react';

export const BETA_FLAGS = ['retitle', 'multiModel'] as const;
export type BetaFlag = (typeof BETA_FLAGS)[number];
export type BetaFlags = Readonly<Record<BetaFlag, boolean>>;

const KEY = 'hl:beta';

export const DEFAULT_BETA: BetaFlags = { retitle: false, multiModel: false } as const satisfies Record<
  BetaFlag,
  boolean
>;

const listeners = new Set<() => void>();
let current: BetaFlags | null = null;

const isFlag = (k: string): k is BetaFlag => (BETA_FLAGS as readonly string[]).includes(k);

/** The saved choices over the defaults: a flag the browser never saved is off. */
export const parseBeta = (raw: string | null): BetaFlags => {
  const out: Record<BetaFlag, boolean> = { ...DEFAULT_BETA };
  if (!raw) return out;
  try {
    const saved: unknown = JSON.parse(raw);
    if (typeof saved !== 'object' || saved === null) return out;
    for (const [k, v] of Object.entries(saved)) if (isFlag(k) && typeof v === 'boolean') out[k] = v;
  } catch {
    // A broken record is the defaults.
  }
  return out;
};

const read = (): BetaFlags => {
  try {
    return parseBeta(window.localStorage.getItem(KEY));
  } catch {
    return DEFAULT_BETA;
  }
};

export const betaSnapshot = (): BetaFlags => (current ??= read());

export const setBeta = (flag: BetaFlag, on: boolean): void => {
  current = { ...betaSnapshot(), [flag]: on };
  try {
    window.localStorage.setItem(KEY, JSON.stringify(current));
  } catch {
    // Private mode or a full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

export const useBeta = (): BetaFlags =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      const onStorage = (event: StorageEvent) => {
        if (event.key !== KEY) return;
        current = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    betaSnapshot,
    () => DEFAULT_BETA,
  );
