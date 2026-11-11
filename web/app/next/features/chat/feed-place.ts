// Where the reader stood in each chat's feed, so going to another chat and back returns them there. A place is a row
// (its key) and how far its top stood under the feed's top edge, not a scroll position: older turns that load above,
// pictures that come and lines that break anew move the pixels but not the row. At the bottom the place is the bottom:
// the feed opens there and follows what comes. Kept for the page's life and in the tab's session storage, for a reload.

export const PLACE = { bottom: 'bottom', at: 'at' } as const;

export type TFeedPlace = { k: typeof PLACE.bottom } | { k: typeof PLACE.at; key: string; offset: number };

export const AT_BOTTOM: TFeedPlace = { k: PLACE.bottom };

const STORAGE_KEY = 'hx-feed-places';
/** The chats whose place is kept; the oldest goes first. */
export const MAX_PLACES = 200;

let places: Map<string, TFeedPlace> | null = null;

const isPlace = (raw: unknown): raw is TFeedPlace => {
  if (typeof raw !== 'object' || raw === null) return false;
  const p = raw as Record<string, unknown>;
  if (p.k === PLACE.bottom) return true;
  return p.k === PLACE.at && typeof p.key === 'string' && typeof p.offset === 'number';
};

const load = (): Map<string, TFeedPlace> => {
  if (places) return places;
  places = new Map();
  try {
    const list: unknown = JSON.parse(window.sessionStorage.getItem(STORAGE_KEY) ?? '[]');
    if (Array.isArray(list))
      for (const item of list)
        if (Array.isArray(item) && typeof item[0] === 'string' && isPlace(item[1]))
          places.set(item[0], item[1]);
  } catch {
    // No storage or a broken entry: the places of this page only.
  }
  return places;
};

let saveTimer = 0;
const save = () => {
  window.clearTimeout(saveTimer);
  saveTimer = window.setTimeout(() => {
    try {
      window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify([...load()]));
    } catch {
      // Private mode or a full storage: the places hold for this page.
    }
  }, 300);
};

/** The place the reader left `chat` at; the bottom for a chat not opened before. */
export const placeOf = (chat: string): TFeedPlace => load().get(chat) ?? AT_BOTTOM;

export const rememberPlace = (chat: string, place: TFeedPlace): void => {
  const map = load();
  const was = map.get(chat);
  if (
    was &&
    was.k === place.k &&
    (place.k === PLACE.bottom || (was.k === PLACE.at && was.key === place.key && was.offset === place.offset))
  )
    return;
  // The latest chat goes last, so the oldest is the first to go.
  map.delete(chat);
  map.set(chat, place);
  while (map.size > MAX_PLACES) map.delete(map.keys().next().value as string);
  save();
};

/** Forgets every place (tests; a person who signs out). */
export const forgetPlaces = (): void => {
  places = new Map();
  try {
    window.sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing to forget.
  }
};

export interface IRowBox {
  key: string;
  top: number;
  bottom: number;
}

/**
 * The reader's place among the drawn rows (in the order they stand), with the feed's top edge at `edge`: the first row
 * that reaches below the edge, and its top under it (negative when the row begins above). Null with no such row.
 */
export const placeAmong = (rows: Iterable<IRowBox>, edge: number): TFeedPlace | null => {
  for (const row of rows) if (row.bottom > edge) return { k: PLACE.at, key: row.key, offset: row.top - edge };
  return null;
};

/** How far to scroll so the row whose top is now at `rowTop` stands where the place says. */
export const scrollToPlace = (place: { offset: number }, rowTop: number, edge: number): number =>
  rowTop - edge - place.offset;
