// What the «Что нового» card shows: one card per entry, the ones not yet told first, then the latest ones told before,
// so the person can leaf back a little.
import { isSeen, type IEntry, type ISeen } from './seen';

/** At most this many untold cards; whoever was away long hears of the latest of them. */
export const MAX_UNSEEN = 12;
/** And this many told ones after them. */
export const MAX_SEEN = 6;

export interface ICard extends IEntry {
  /** Not told yet: it goes first and carries «Новое». */
  unseen: boolean;
}

/** Newest day first; within a day by id, so the order never depends on the file system. */
export const byNewest = (a: IEntry, b: IEntry): number =>
  a.date === b.date ? (a.id < b.id ? -1 : a.id > b.id ? 1 : 0) : a.date < b.date ? 1 : -1;

/**
 * The cards for this person, or null when there is nothing new. A browser that was told of nothing hears of the newest
 * day alone, not of the whole history.
 */
export const newsFor = (entries: readonly IEntry[], seen: ISeen | null): ICard[] | null => {
  const sorted = [...entries].sort(byNewest);
  const newest = sorted[0];
  if (newest === undefined) return null;
  const fresh = sorted.filter((entry) => (seen === null ? entry.date === newest.date : !isSeen(seen, entry)));
  if (fresh.length === 0) return null;
  const told = sorted.filter((entry) => !fresh.includes(entry));
  return [
    ...fresh.slice(0, MAX_UNSEEN).map((entry) => ({ ...entry, unseen: true })),
    ...told.slice(0, MAX_SEEN).map((entry) => ({ ...entry, unseen: false })),
  ];
};
