// The cards an app posted into a chat with hl.chat.card(view, data). The chat's feed is the agent's transcript, which
// the page cannot write into, so a posted card stands at the end of the feed, under the last turn, and is kept with the
// chat in this browser (a few, newest last). The card is drawn by the app's own view through the same bridge as a
// card of a tool step.
import { useSyncExternalStore } from 'react';

export interface IPostedCard {
  id: string;
  app: string;
  view: string;
  data: unknown;
  at: number;
}

/** A chat keeps this many posted cards; the oldest go. */
export const CARDS_MAX = 20;
/** The data of one card, as JSON: more is not a card, it is a document. */
export const CARD_DATA_MAX = 20_000;

const KEY = 'hx-app-cards';
const listeners = new Set<() => void>();
const NONE: readonly IPostedCard[] = [];

let cache: Map<string, readonly IPostedCard[]> | null = null;

const isCard = (v: unknown): v is IPostedCard => {
  if (typeof v !== 'object' || v === null) return false;
  const c = v as Record<string, unknown>;
  return (
    typeof c.id === 'string' &&
    typeof c.app === 'string' &&
    typeof c.view === 'string' &&
    typeof c.at === 'number' &&
    'data' in c
  );
};

/** The kept cards of every chat, or nothing when what was kept is not them. */
export const parseCards = (raw: string | null): Map<string, readonly IPostedCard[]> => {
  const out = new Map<string, readonly IPostedCard[]>();
  if (raw === null) return out;
  try {
    const v: unknown = JSON.parse(raw);
    if (typeof v !== 'object' || v === null) return out;
    for (const [chat, list] of Object.entries(v)) {
      if (Array.isArray(list)) out.set(chat, list.filter(isCard).slice(-CARDS_MAX));
    }
  } catch {
    // A broken entry: nothing was kept.
  }
  return out;
};

const all = (): Map<string, readonly IPostedCard[]> => {
  if (cache) return cache;
  try {
    cache = parseCards(localStorage.getItem(KEY));
  } catch {
    cache = new Map();
  }
  return cache;
};

const save = () => {
  try {
    localStorage.setItem(KEY, JSON.stringify(Object.fromEntries(all())));
  } catch {
    // Blocked or full storage: the cards hold while the page is open.
  }
};

let seq = 0;

/** Posts a card into a chat; false for data that is too big to be one. */
export const postCard = (chat: string, card: { app: string; view: string; data: unknown }): boolean => {
  const size = JSON.stringify(card.data ?? null).length;
  if (size > CARD_DATA_MAX) return false;
  const kept = all();
  const next = [
    ...(kept.get(chat) ?? NONE),
    { ...card, id: `${Date.now().toString(36)}${(++seq).toString(36)}`, at: Date.now() },
  ];
  kept.set(chat, next.slice(-CARDS_MAX));
  save();
  listeners.forEach((l) => l());
  return true;
};

/** Forgets the cache (tests). */
export const resetCards = () => {
  cache = null;
  listeners.forEach((l) => l());
};

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};

export const usePostedCards = (chat: string | null): readonly IPostedCard[] =>
  useSyncExternalStore(
    subscribe,
    () => (chat === null ? NONE : (all().get(chat) ?? NONE)),
    () => NONE,
  );
