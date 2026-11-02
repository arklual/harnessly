// What a view called, for the app inspector: the method, the right it needed and how it ended. Kept per app and chat
// while the page is open; a view's data never goes in, only the shape of the call.
import { useSyncExternalStore } from 'react';
import type { IBridgeLogEntry } from '@/next/features/app-views/bridge';

/** More than this is not read; the oldest go. */
export const LOG_MAX = 200;

const logs = new Map<string, readonly IBridgeLogEntry[]>();
const listeners = new Set<() => void>();
const NONE: readonly IBridgeLogEntry[] = [];

export const logKey = (app: string, conversation: string | null): string => `${app}|${conversation ?? '-'}`;

export const logCall = (key: string, entry: IBridgeLogEntry): void => {
  const next = [...(logs.get(key) ?? NONE), entry];
  logs.set(key, next.length > LOG_MAX ? next.slice(next.length - LOG_MAX) : next);
  listeners.forEach((l) => l());
};

/** The calls kept for an app in a chat, oldest first. */
export const callsOf = (key: string): readonly IBridgeLogEntry[] => logs.get(key) ?? NONE;

export const clearLog = (key: string): void => {
  logs.delete(key);
  listeners.forEach((l) => l());
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

export const useBridgeLog = (key: string): readonly IBridgeLogEntry[] =>
  useSyncExternalStore(
    subscribe,
    () => callsOf(key),
    () => NONE,
  );
