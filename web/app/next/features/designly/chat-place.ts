// A design and the chat that edits it are one place (one tab): the person who went into «Редактирование», or into the
// design's chat, works in Sinopia with the chat at its side or over it. This browser keeps, for each such chat, which
// design it edits, on which machine and on which page it was left, so a chat opened from the sidebar, the search or a
// notification comes back as that design with its chat, where the person left it, and not as a plain chat with the
// design in a second tab. A design only looked at («Просмотр»), with no chat, stays a place of its own.
import { ROUTE, type TRoute } from '@/next/routes/routes';

export interface IChatPlace {
  design: string;
  machine: string | null;
  page: string | null;
}

const KEY = 'hx-design-chat-places';
/** More pairs than this are not a working set; the least recent are forgotten. */
export const MAX_PLACES = 100;

let places: Map<string, IChatPlace> | null = null;

const isPlace = (v: unknown): v is IChatPlace => {
  if (typeof v !== 'object' || v === null) return false;
  const p = v as Record<string, unknown>;
  return (
    typeof p.design === 'string' &&
    (p.machine === null || typeof p.machine === 'string') &&
    (p.page === null || typeof p.page === 'string')
  );
};

/** The kept pairs, oldest first, or none when nothing (or nothing of ours) was kept. */
export const parsePlaces = (raw: string | null): Map<string, IChatPlace> => {
  const out = new Map<string, IChatPlace>();
  if (raw === null) return out;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return out;
    for (const [conv, value] of Object.entries(parsed)) if (isPlace(value)) out.set(conv, value);
  } catch {
    // A broken value: nothing was kept.
  }
  return out;
};

const read = (): Map<string, IChatPlace> => {
  if (places) return places;
  try {
    places = parsePlaces(localStorage.getItem(KEY));
  } catch {
    places = new Map();
  }
  return places;
};

/** The chat edits this design: it is kept last, the oldest beyond MAX_PLACES dropped. */
export const keepChatPlace = (conv: string, place: IChatPlace): void => {
  const all = read();
  const was = all.get(conv);
  if (was && was.design === place.design && was.machine === place.machine && was.page === place.page) return;
  all.delete(conv);
  all.set(conv, place);
  while (all.size > MAX_PLACES) all.delete(all.keys().next().value as string);
  try {
    localStorage.setItem(KEY, JSON.stringify(Object.fromEntries(all)));
  } catch {
    // Full or blocked storage: it lives for this page.
  }
};

export const chatPlaceOf = (conv: string): IChatPlace | null => read().get(conv) ?? null;

/** «Открыть чат» of the design's column asks for the plain chat once: that one opening is not turned into the design. */
const plain = new Set<string>();
export const openPlainOnce = (conv: string): void => {
  plain.add(conv);
};

/**
 * Where an address is shown: a chat that edits a design is that design in «Редактирование» with its chat, on the page
 * it was left on. Anything else is shown as it is.
 */
export const designPlaceOf = (
  route: TRoute,
  place: (conv: string) => IChatPlace | null = chatPlaceOf,
): TRoute => {
  if (route.kind !== ROUTE.chat || plain.has(route.id)) return route;
  const kept = place(route.id);
  if (!kept) return route;
  return {
    kind: ROUTE.site,
    id: kept.design,
    chat: route.id,
    edit: true,
    ...(kept.page !== null ? { page: kept.page } : {}),
    ...(kept.machine !== null ? { machine: kept.machine } : {}),
  };
};

/** The plain chat was left: the next opening of it is the design again. */
export const clearPlain = (except: string | null = null): void => {
  for (const conv of [...plain]) if (conv !== except) plain.delete(conv);
};

/** Only for tests. */
export const resetChatPlaces = (): void => {
  places = null;
  plain.clear();
};
