// «Viewed» marks of the review: per person, per file path, per version of the file. A mark holds the version it was
// put on, so a file that changed since reads as not viewed again. Kept in this browser; a missing or broken storage
// only means the marks last as long as the page.
import { useCallback, useSyncExternalStore } from 'react';
import type { IReviewFile } from '@/next/features/review/model';

export type TViewedMarks = Readonly<Record<string, string>>;

const PREFIX = 'hx.review.viewed:';
/** Marks kept per person; the oldest go first past this. */
export const VIEWED_MAX = 5000;

export const viewedStorageKey = (person: string): string => `${PREFIX}${person}`;

export const parseViewed = (raw: string | null): TViewedMarks => {
  if (!raw) return {};
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    const out: Record<string, string> = {};
    for (const [key, version] of Object.entries(value)) if (typeof version === 'string') out[key] = version;
    return out;
  } catch {
    return {};
  }
};

/** A mark's key: the change's scope (repository or project) and the file path. */
export const markKey = (scope: string, path: string): string => `${scope}\0${path}`;

export const isViewed = (marks: TViewedMarks, scope: string, file: IReviewFile): boolean =>
  marks[markKey(scope, file.path)] === file.version;

/** The marks with this file marked or unmarked at its current version. */
export const setViewed = (
  marks: TViewedMarks,
  scope: string,
  file: IReviewFile,
  viewed: boolean,
): TViewedMarks => {
  const key = markKey(scope, file.path);
  const next: Record<string, string> = { ...marks };
  delete next[key];
  if (viewed) next[key] = file.version;
  const keys = Object.keys(next);
  for (const old of keys.slice(0, Math.max(0, keys.length - VIEWED_MAX))) delete next[old];
  return next;
};

const cache = new Map<string, TViewedMarks>();
const listeners = new Set<() => void>();

const read = (person: string): TViewedMarks => {
  const key = viewedStorageKey(person);
  let marks = cache.get(key);
  if (marks) return marks;
  try {
    marks = parseViewed(window.localStorage.getItem(key));
  } catch {
    marks = {};
  }
  cache.set(key, marks);
  return marks;
};

const write = (person: string, marks: TViewedMarks) => {
  const key = viewedStorageKey(person);
  cache.set(key, marks);
  try {
    window.localStorage.setItem(key, JSON.stringify(marks));
  } catch {
    // Private mode or a full storage: the marks hold for this page.
  }
  for (const listener of [...listeners]) listener();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

/** The person's marks within one scope and a way to toggle one. */
export const useViewed = (person: string, scope: string) => {
  const marks = useSyncExternalStore(subscribe, () => read(person));
  const toggle = useCallback(
    (file: IReviewFile, viewed: boolean) => write(person, setViewed(read(person), scope, file, viewed)),
    [person, scope],
  );
  const viewed = useCallback((file: IReviewFile) => isViewed(marks, scope, file), [marks, scope]);
  return { viewed, toggle };
};

/** Tests start from an empty cache. */
export const resetViewedCache = () => cache.clear();
