// «Что показывать в ленте» (Settings → General): which things a turn made get a card under its answer. This browser's,
// as the theme and the pane's own switch are. The plain files are off unless the person turns them on (what
// the agent made matters, not that it wrote a file).
import { useSyncExternalStore } from 'react';
import { OUTPUT_KINDS } from '@/next/protocol/v2';

export const FEED_KINDS = [...OUTPUT_KINDS, 'file'] as const;
export type FeedKind = (typeof FEED_KINDS)[number];
export type FeedKinds = Readonly<Record<FeedKind, boolean>>;

const KEY = 'hl:feed-kinds';

export const DEFAULT_FEED_KINDS: FeedKinds = {
  artifact: true,
  design: true,
  doc: true,
  mr: true,
  stand: true,
  delivery: true,
  file: false,
} as const satisfies Record<FeedKind, boolean>;

const listeners = new Set<() => void>();
let current: FeedKinds | null = null;

const isKind = (k: string): k is FeedKind => (FEED_KINDS as readonly string[]).includes(k);

/** The saved choices over the defaults: a kind the browser never saved is its default. */
export const parseFeedKinds = (raw: string | null): FeedKinds => {
  const out: Record<FeedKind, boolean> = { ...DEFAULT_FEED_KINDS };
  if (!raw) return out;
  try {
    const saved: unknown = JSON.parse(raw);
    if (typeof saved !== 'object' || saved === null) return out;
    for (const [k, v] of Object.entries(saved)) if (isKind(k) && typeof v === 'boolean') out[k] = v;
  } catch {
    // A broken record is the defaults.
  }
  return out;
};

const read = (): FeedKinds => {
  try {
    return parseFeedKinds(window.localStorage.getItem(KEY));
  } catch {
    return DEFAULT_FEED_KINDS;
  }
};

export const feedKindsSnapshot = (): FeedKinds => (current ??= read());

export const setFeedKind = (kind: FeedKind, on: boolean): void => {
  current = { ...feedKindsSnapshot(), [kind]: on };
  try {
    window.localStorage.setItem(KEY, JSON.stringify(current));
  } catch {
    // Private mode or a full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

export const useFeedKinds = (): FeedKinds =>
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
    feedKindsSnapshot,
    () => DEFAULT_FEED_KINDS,
  );
