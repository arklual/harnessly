// A place in a file the agent asked to show: waits here for the file's view, which may be a moment from being open or from
// having read the file, and which takes it away once it has gone there.
import { createKeyedStore, useKey } from '@/next/core/store';

const places = createKeyedStore<string, string>();

export const requestFilePlace = (path: string, place: string): void => places.set(path, place);

export const useFilePlace = (path: string): string | null => useKey(places, path) ?? null;

export const settleFilePlace = (path: string): void => places.patch(path, () => undefined);

/** `12.5`, `12.5-18`: the start and the end of a stretch; null for what is no time. */
export const parseTimePlace = (place: string): { from: number; to: number | null } | null => {
  const found = place.trim().match(/^(\d+(?:\.\d+)?)(?:\s*-\s*(\d+(?:\.\d+)?))?$/);
  if (!found) return null;
  const from = Number(found[1]);
  return { from, to: found[2] === undefined ? null : Math.max(from, Number(found[2])) };
};

/** `7`: a page from 1; null for what is no page. */
export const parsePagePlace = (place: string): number | null =>
  /^\d+$/.test(place.trim()) && Number(place) >= 1 ? Number(place) : null;
