// The deck's comment list as the panel and the pins show it: the team's comments (the runtime's meta/comments) and the
// guests' (the gateway's owner route) merged into one list in the order they were left, numbered by that order. The
// number is the pin's and the row's bubble: it does not change when another comment is resolved. No React here.
import type { IGuestCommentOwned } from '@/next/features/designly/access/api';
import { DECK_GUEST_PREFIX, type IDeckComment, type IDeckSlide } from '@/next/features/designly/deck/types';

export interface IDeckCommentItem {
  /** The id the resolve route takes: the team's ULID, or `g-<id>` of a guest's. */
  key: string;
  guest: boolean;
  /** 1-based number in the list. */
  n: number;
  /** 0-based index of the slide in the deck. */
  slide: number;
  /** The author as shown; null for a guest without a name (the caller words it). */
  who: string | null;
  text: string;
  /** Percent of the slide, null for a guest's comment that has no point (no pin). */
  x: number | null;
  y: number | null;
  label: string;
  resolved: boolean;
  at: string;
}

const whoOf = (c: IDeckComment): string => (c.by.name !== '' ? c.by.name : c.by.email.split('@')[0]);

/** The merged list; a comment on a slide the deck no longer has is left out. */
export const mergeComments = (
  slides: readonly IDeckSlide[],
  team: readonly IDeckComment[],
  guests: readonly IGuestCommentOwned[] | null,
  guestResolved: readonly string[],
): IDeckCommentItem[] => {
  const at = new Map(slides.map((s, i) => [s.id, i]));
  const byPath = new Map(slides.map((s, i) => [s.path, i]));
  const items: Omit<IDeckCommentItem, 'n'>[] = [];
  for (const c of team) {
    const slide = at.get(c.page);
    if (slide === undefined) continue;
    items.push({
      key: c.id,
      guest: false,
      slide,
      who: whoOf(c),
      text: c.text,
      x: c.x,
      y: c.y,
      label: c.label,
      resolved: c.resolved,
      at: c.at,
    });
  }
  const done = new Set(guestResolved);
  for (const g of guests ?? []) {
    const slide = byPath.get(g.board);
    if (slide === undefined) continue;
    const key = `${DECK_GUEST_PREFIX}${g.id}`;
    items.push({
      key,
      guest: true,
      slide,
      who: g.name === '' ? null : g.name,
      text: g.text,
      x: g.x === null ? null : g.x * 100,
      y: g.y === null ? null : g.y * 100,
      label: g.label,
      resolved: done.has(key),
      at: g.at,
    });
  }
  // ISO times order as text; a stable sort keeps the team's before a guest's of the same second.
  items.sort((a, b) => (a.at < b.at ? -1 : a.at > b.at ? 1 : 0));
  return items.map((item, i) => ({ ...item, n: i + 1 }));
};

/** The badge of the «Комментарии» tab and the phone's round button: the unresolved. */
export const unresolvedCount = (items: readonly IDeckCommentItem[]): number =>
  items.filter((c) => !c.resolved).length;

/** The pins of a slide: unresolved comments that have a point. */
export const pinsOf = (items: readonly IDeckCommentItem[], slide: number): IDeckCommentItem[] =>
  items.filter((c) => c.slide === slide && !c.resolved && c.x !== null && c.y !== null);

/** The number a comment being written gets: the next one. */
export const nextNumber = (items: readonly IDeckCommentItem[]): number => items.length + 1;
