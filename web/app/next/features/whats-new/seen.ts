// What this browser has already been told of in «Что нового». Kept apart from the entries (no import.meta.glob here),
// so the browser tests can import SEEN_ALL and keep the card away.

export const SEEN_KEY = 'hx:whats-new';

/** An entry as the card needs it: its texts are next.whatsNew.<id>.title / .text of the dictionary. */
export interface IEntry {
  /** The file name in entries/ without .json. */
  id: string;
  /** The day it went out, YYYY-MM-DD. */
  date: string;
  icon: string;
}

/** Everything up to `upTo` (a YYYY-MM-DD day) was told, and so were `ids` of the days after it. */
export interface ISeen {
  upTo: string;
  ids: readonly string[];
}

/** Everything is told: what the browser tests put into storage, so the card never comes in their way. */
export const SEEN_ALL = JSON.stringify({ upTo: '9999-12-31', ids: [] } satisfies ISeen);

/**
 * The stored value. Before entries became files the id of the newest release seen was kept, a day with a letter
 * (2026-11-11zd): everything of that day and before counts as told.
 */
export const parseSeen = (raw: string | null): ISeen | null => {
  if (raw === null || raw === '') return null;
  if (!raw.startsWith('{')) return /^\d{4}-\d\d-\d\d/.test(raw) ? { upTo: raw.slice(0, 10), ids: [] } : null;
  try {
    const value = JSON.parse(raw) as { upTo?: unknown; ids?: unknown };
    if (typeof value.upTo !== 'string') return null;
    const ids = Array.isArray(value.ids)
      ? value.ids.filter((id): id is string => typeof id === 'string')
      : [];
    return { upTo: value.upTo, ids };
  } catch {
    return null;
  }
};

export const isSeen = (seen: ISeen, entry: IEntry): boolean =>
  entry.date <= seen.upTo || seen.ids.includes(entry.id);

/** How far back by its own day an entry may still come in late (a branch merged days after it was written). */
export const LATE_DAYS = 30;

const dayBefore = (date: string, days: number): string => {
  const at = new Date(`${date}T00:00:00Z`);
  at.setUTCDate(at.getUTCDate() - days);
  return at.toISOString().slice(0, 10);
};

/**
 * All of `entries` told, kept small: the days long past by the day alone, the last month by id, so an entry written
 * earlier and merged later is still news.
 */
export const seenAll = (entries: readonly IEntry[]): ISeen => {
  const newest = entries.reduce((max, entry) => (entry.date > max ? entry.date : max), '');
  if (newest === '') return { upTo: '', ids: [] };
  const upTo = dayBefore(newest, LATE_DAYS);
  return { upTo, ids: entries.filter((entry) => entry.date > upTo).map((entry) => entry.id) };
};
