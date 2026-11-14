// The right column over a change of chat: it stays where it is and changes what it shows. The last chat's content lies
// over the next chat's and fades out while the next one fades in (SWAP_MS), and the column's height goes over from the
// old to the new. What the next chat has is drawn at once from memory (its recap is on the chat's record, its pictures stay for the
// recent chats, stores/cache.ts); what has not come yet stands as a skeleton of the same block, not as an empty column.
import { useEffect, useState } from 'react';
import { useGlide } from '@/next/ui/use-glide';
import type { BackgroundTask, Recap, Shot } from '@/next/protocol/v2';

/** The cross-fade of two chats' content, and the column's change of height with it. */
export const SWAP_MS = 180;

/** What the column shows of one chat. */
export interface IChatContent {
  chat: string | null;
  tasks: readonly BackgroundTask[];
  recap: Recap | null;
  shots: readonly Shot[] | null;
  shotSrc: (shot: Shot) => string;
}

const sameList = <T>(a: readonly T[] | null, b: readonly T[] | null): boolean =>
  a === b ||
  (a !== null && b !== null && a.length === b.length && a.every((item, index) => item === b[index]));

/** By reference, items of a list one by one: the stores keep an item's object while it does not change. */
const sameContent = (a: IChatContent, b: IChatContent): boolean =>
  a.chat === b.chat &&
  sameList(a.tasks, b.tasks) &&
  a.recap === b.recap &&
  sameList(a.shots, b.shots) &&
  a.shotSrc === b.shotSrc;

/**
 * What to draw: the next chat's content, and for SWAP_MS after a change of chat the last chat's (`leaving`), which lies
 * over it and fades out while the next one fades in — a cross-fade, never an empty column between them.
 */
export const useChatSwap = (next: IChatContent): { view: IChatContent; leaving: IChatContent | null } => {
  const [view, setView] = useState(next);
  const [leaving, setLeaving] = useState<IChatContent | null>(null);
  // State adjusted during render, not in an effect: the frame that has the new chat already draws the old one leaving.
  if (next.chat !== view.chat) {
    setLeaving(view);
    setView(next);
  } else if (!sameContent(next, view)) {
    setView(next);
  }

  useEffect(() => {
    if (!leaving) return;
    const timer = setTimeout(() => setLeaving(null), SWAP_MS);
    return () => clearTimeout(timer);
  }, [leaving]);

  // A chat that came back while it was leaving does not lie over itself.
  return { view: next, leaving: leaving && leaving.chat !== next.chat ? leaving : null };
};

/** The column's height goes over from the last chat's to the next chat's (ui/use-glide.ts). */
export const useSwapHeight = (box: HTMLElement | null, chat: string | null): void =>
  useGlide(box, chat ?? '', 'height', SWAP_MS);
