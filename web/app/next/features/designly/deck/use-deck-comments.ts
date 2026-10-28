// The deck's comments for the panel, the badge and the pins: the team's from the deck state, the guests' read from the
// gateway (./guest-comments.ts), merged by ./comments-model.ts.
// Resolving and adding are pessimistic: the list changes with the runtime's answer, not before it.
import { useMemo } from 'react';
import { RuntimeError } from '@/next/core/net/runtime-json';
import {
  addDeckComment,
  resolveDeckComment,
  type IDeckCommentDraft,
} from '@/next/features/designly/deck/api';
import {
  mergeComments,
  unresolvedCount,
  type IDeckCommentItem,
} from '@/next/features/designly/deck/comments-model';
import { useGuestComments } from '@/next/features/designly/deck/guest-comments';
import { useDeck, putDeckComment, putGuestResolved } from '@/next/stores/deck';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

export interface IDeckComments {
  /** null while the deck has not been read. */
  items: readonly IDeckCommentItem[] | null;
  /** Unresolved: the badge. */
  open: number;
  /** Flips the resolved state; true when the runtime took it. */
  resolve: (item: IDeckCommentItem) => Promise<boolean>;
  /** Adds a team comment on `slide` (0-based); true when the runtime took it. */
  add: (slide: number, draft: Omit<IDeckCommentDraft, 'page'>) => Promise<boolean>;
}

const failed = (failure: unknown) =>
  toast(failure instanceof RuntimeError ? failure.message : t('next.failure.http'), { tone: 'error' });

export const useDeckComments = (machine: MachineId, artifact: string): IDeckComments => {
  const deck = useDeck(artifact);
  const guests = useGuestComments(artifact, deck !== undefined);
  const items = useMemo(
    () => (deck ? mergeComments(deck.slides, deck.comments, guests, deck.guestResolved) : null),
    [deck, guests],
  );

  const resolve = async (item: IDeckCommentItem) => {
    try {
      const done = await resolveDeckComment(machine, artifact, item.key, !item.resolved);
      if (item.guest) putGuestResolved(artifact, item.key, done.resolved);
      else if (done.comment) putDeckComment(artifact, done.comment);
      return true;
    } catch (failure) {
      failed(failure);
      return false;
    }
  };

  const add = async (slide: number, draft: Omit<IDeckCommentDraft, 'page'>) => {
    const page = deck?.slides[slide]?.id;
    if (!page) return false;
    try {
      putDeckComment(artifact, await addDeckComment(machine, artifact, { ...draft, page }));
      return true;
    } catch (failure) {
      failed(failure);
      return false;
    }
  };

  return { items, open: items ? unresolvedCount(items) : 0, resolve, add };
};
