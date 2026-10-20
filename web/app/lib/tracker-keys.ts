import { issueUrlOf } from '@/lib/site';
import { useSyncExternalStore } from 'react';
import { appFetch } from '@/lib/app-path';

// Known before the tracker answers (none until it does); the list then comes from the tracker itself.
let keys: string[] = [];
let requested = false;
const listeners = new Set<() => void>();

const escape = (key: string) => key.replace(/[^A-Z0-9]/g, '');

/** Matches `KEY-123` for any tracker project, not inside words, paths or longer tokens. */
export const issueKeyRe = () =>
  new RegExp(`(?<![\\w/.-])((?:${keys.map(escape).join('|')})-\\d{1,6})(?![\\w-])`, 'g');
export const issueUrl = (key: string) => issueUrlOf(key) ?? '';

export const setTrackerKeys = (next: string[]) => {
  if (!next.length || next.join() === keys.join()) return;
  keys = next;
  listeners.forEach((notify) => notify());
};

const subscribe = (notify: () => void) => {
  listeners.add(notify);
  if (!requested && typeof window !== 'undefined') {
    requested = true;
    appFetch('/api/platform/link-meta/projects')
      .then((r) => (r.ok ? (r.json() as Promise<{ keys: string[] }>) : null))
      .then((data) => data && setTrackerKeys(data.keys))
      .catch(() => {});
  }
  return () => {
    listeners.delete(notify);
  };
};

/** Re-renders when the project list arrives, so keys of new projects light up too. */
export const useTrackerKeys = () =>
  useSyncExternalStore(
    subscribe,
    () => keys,
    () => keys,
  );
