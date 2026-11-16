// A design and the chat that edits it are one place (one tab): the person who went into «Редактирование», or into the
// design's chat, works in Sinopia with the chat at its side or over it. This browser keeps, for each such chat, which
// design it edits, on which machine and on which page it was left, so a chat opened from the sidebar, the search or a
// notification comes back as that design with its chat, where the person left it, and not as a plain chat with the
// design in a second tab. A design only looked at («Просмотр»), with no chat, stays a place of its own.
// «К чату» is the person's choice for that chat: it stays a plain chat — after another chat, after a reload — until the
// design is opened with it again (by the person, or by the agent asked to edit it, design-full.ts).
import { ROUTE, type TRoute } from '@/next/routes/routes';

export interface IChatPlace {
  design: string;
  machine: string | null;
  page: string | null;
  /** The person chose the plain chat («К чату»): it is not turned into the design. */
  asChat?: boolean;
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
    (p.page === null || typeof p.page === 'string') &&
    (p.asChat === undefined || typeof p.asChat === 'boolean')
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

const write = (all: Map<string, IChatPlace>) => {
  while (all.size > MAX_PLACES) all.delete(all.keys().next().value as string);
  try {
    localStorage.setItem(KEY, JSON.stringify(Object.fromEntries(all)));
  } catch {
    // Full or blocked storage: it lives for this page.
  }
};

/** The chat edits this design (it is on screen with it): it is kept last, the oldest beyond MAX_PLACES dropped. */
export const keepChatPlace = (conv: string, place: IChatPlace): void => {
  const all = read();
  const was = all.get(conv);
  if (
    was &&
    !was.asChat &&
    was.design === place.design &&
    was.machine === place.machine &&
    was.page === place.page
  )
    return;
  all.delete(conv);
  all.set(conv, { design: place.design, machine: place.machine, page: place.page });
  write(all);
};

export const chatPlaceOf = (conv: string): IChatPlace | null => read().get(conv) ?? null;

/** «К чату» of the design's column: the chat is shown as a plain chat from now on, until the design opens with it again. */
export const chooseChat = (conv: string): void => {
  const all = read();
  const was = all.get(conv);
  if (!was || was.asChat) return;
  all.delete(conv);
  all.set(conv, { ...was, asChat: true });
  write(all);
};

/**
 * Where an address is shown: a chat that edits a design is that design in «Редактирование» with its chat, on the page
 * it was left on. Anything else is shown as it is.
 */
export const designPlaceOf = (
  route: TRoute,
  place: (conv: string) => IChatPlace | null = chatPlaceOf,
): TRoute => {
  if (route.kind !== ROUTE.chat) return route;
  const kept = place(route.id);
  if (!kept || kept.asChat) return route;
  return {
    kind: ROUTE.site,
    id: kept.design,
    chat: route.id,
    edit: true,
    ...(kept.page !== null ? { page: kept.page } : {}),
    ...(kept.machine !== null ? { machine: kept.machine } : {}),
  };
};

/** Only for tests. */
export const resetChatPlaces = (): void => {
  places = null;
};
