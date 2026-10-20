import { useEffect, useSyncExternalStore } from 'react';
import type { Conversation } from '@/lib/hl-conversations';

/**
 * Last seen entry of every pinned or project conversation on this device, so a row whose machine is away
 * keeps its title instead of turning into «Conversation on machine». Display only: state never comes from here.
 */
const STORAGE_KEY = 'hl:sidebar-known';
const MAX_ENTRIES = 300;

type TKnown = Record<string, Conversation>;

const EMPTY: TKnown = {};

let known: TKnown | null = null;
const listeners = new Set<() => void>();

const readKnown = (): TKnown => {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null');
    return typeof saved === 'object' && saved !== null ? (saved as TKnown) : EMPTY;
  } catch {
    return EMPTY;
  }
};

const getSnapshot = (): TKnown => {
  known ??= readKnown();
  return known;
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

const pick = ({
  key,
  id,
  title,
  updatedAt,
  provider,
  kind,
  machineId,
  machineName,
  workspaceId,
  mode,
}: Conversation): Conversation => ({
  key,
  id,
  title,
  updatedAt,
  provider,
  kind,
  machineId,
  machineName,
  workspaceId,
  mode,
});

const remember = (conversations: readonly Conversation[], wanted: ReadonlySet<string>) => {
  const current = getSnapshot();
  const next: TKnown = {};
  for (const ref of wanted) if (current[ref]) next[ref] = current[ref];
  for (const item of conversations) if (wanted.has(item.key)) next[item.key] = pick(item);
  const result = Object.fromEntries(Object.entries(next).slice(-MAX_ENTRIES));
  if (JSON.stringify(result) === JSON.stringify(current)) return;
  known = result;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(result));
  } catch {
    // Titles of away machines are a convenience; the rows still render without them.
  }
  for (const listener of listeners) listener();
};

/** `refs` is null while the organizer loads: the cache is left alone until it is known what to keep. */
const useKnownConversations = (
  conversations: readonly Conversation[],
  refs: readonly string[] | null,
): TKnown => {
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, () => EMPTY);
  const refsKey = refs === null ? null : refs.join('\n');
  useEffect(() => {
    if (refsKey !== null) remember(conversations, new Set(refsKey ? refsKey.split('\n') : []));
  }, [conversations, refsKey]);
  return snapshot;
};

export default useKnownConversations;
