// Which guest links of a project give access now, revoking them all, and how the PDF they offer is doing. Pure but
// for the revoke call, given.
import type { IGuestLink } from '@/next/features/designly/access/api';
import { readDeckLinkView } from '@/next/features/designly/deck/deck-link-view';
import type { IDeckPublished } from '@/next/features/designly/deck/types';

/** A link that is still good: not revoked, not run out. */
export const isLive = (link: IGuestLink, now: number): boolean =>
  link.revokedAt === null && (link.expiresAt === null || new Date(link.expiresAt).getTime() > now);

/** Every link of the project that opens the deck without signing in, whatever it was made for. */
export const liveLinks = (links: readonly IGuestLink[], now: number): IGuestLink[] =>
  links.filter((l) => isLive(l, now));

/** The link the popover's flags belong to: the first live one that carries a deck mode. */
export const deckLinkOf = (live: readonly IGuestLink[]): IGuestLink | null =>
  live.find((l) => readDeckLinkView(l.view) !== null) ?? null;

/** Live links that are not the deck's link: the popover cannot show their settings, only that they give access. */
export const otherLinks = (live: readonly IGuestLink[]): IGuestLink[] => {
  const deck = deckLinkOf(live);
  return live.filter((l) => l !== deck);
};

export interface IRevokeOutcome {
  revoked: string[];
  failed: string[];
}

/** Revokes every given link; none is skipped when one fails, and the outcome says which went and which stayed. */
export const revokeAll = async (
  live: readonly IGuestLink[],
  revoke: (id: string) => Promise<void>,
): Promise<IRevokeOutcome> => {
  const results = await Promise.allSettled(live.map((l) => revoke(l.id)));
  const out: IRevokeOutcome = { revoked: [], failed: [] };
  results.forEach((r, i) => (r.status === 'fulfilled' ? out.revoked : out.failed).push(live[i].id));
  return out;
};

/** How the PDF guests download is doing, as one quiet line says it. */
export type TPdfLine =
  { k: 'building' } | { k: 'updating' } | { k: 'ready'; at: number } | { k: 'failed'; reason: string };

/** The line for the deck's published state; null while there is nothing to say (no PDF and none coming). */
export const pdfLineOf = (p: IDeckPublished): TPdfLine | null => {
  if (p.pdfBuilding) return p.pdf ? { k: 'updating' } : { k: 'building' };
  if (p.pdfError !== null) return { k: 'failed', reason: p.pdfError };
  if (p.pdf && p.pdfAt !== null) return { k: 'ready', at: Date.parse(p.pdfAt) };
  return null;
};
