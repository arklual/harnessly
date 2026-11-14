// The one way a marked picture (markup.ts) reaches the field: whatever place it was opened from (a picture
// of the feed, a generated card, a screenshot, a file of the pane, an attachment of the field) hands the new file here,
// and the field of the chat takes it as an attachment; a picture that was an attachment of the field takes its place.
// The places know nothing of the field: a chat's field (bindings/chat-dock.tsx) and the new chat's
// (bindings/home-composer.tsx) take what is addressed to them or to nobody in particular.
import { useEffect, useMemo, useRef } from 'react';
import type { DraftKey } from '@/next/stores/drafts';

export interface IMarkedPicture {
  file: File;
  /** The field's attachment it takes the place of. */
  replace?: string;
  /** The field it goes to; absent: the field on screen. */
  to?: DraftKey;
}

/** What the editor needs to give its result away: whether it replaces an attachment, and where it goes. */
export interface IMarkupTarget {
  replacing: boolean;
  give: (file: File) => void;
}

/** What a field does with a marked picture: its uploads' own add and remove. */
export interface IMarkedTaker {
  onFiles: (files: File[]) => void;
  onRemove: (id: string) => void;
  /** The field has this attachment: a picture that replaces it is this field's. */
  owns: (id: string) => boolean;
}

const waiting: IMarkedPicture[] = [];
const listeners = new Set<() => void>();

/** Hands a marked picture to the field; it waits for one if none is on screen. */
export const toComposer = (picture: IMarkedPicture): void => {
  waiting.push(picture);
  for (const listener of [...listeners]) listener();
};

/**
 * Takes out what is addressed to `key` (or to nobody) and gives it to the field, in the order it came; a picture that
 * replaces an attachment goes only to the field that has it.
 */
export const takeMarked = (key: DraftKey, taker: IMarkedTaker): number => {
  let taken = 0;
  for (let i = 0; i < waiting.length;) {
    const picture = waiting[i];
    const mine =
      picture.replace === undefined
        ? picture.to === undefined || picture.to === key
        : taker.owns(picture.replace);
    if (!mine) {
      i += 1;
      continue;
    }
    waiting.splice(i, 1);
    if (picture.replace !== undefined) taker.onRemove(picture.replace);
    taker.onFiles([picture.file]);
    taken += 1;
  }
  return taken;
};

/** For tests: forgets what waits. */
export const resetMarked = (): void => {
  waiting.length = 0;
};

/** A field takes the marked pictures meant for it, now and whenever one comes. */
export const useTakeMarked = (key: DraftKey | null, taker: IMarkedTaker): void => {
  const latest = useRef(taker);
  useEffect(() => {
    latest.current = taker;
  });
  useEffect(() => {
    if (key === null) return;
    const take = () => takeMarked(key, latest.current);
    take();
    listeners.add(take);
    return () => {
      listeners.delete(take);
    };
  }, [key]);
};

/** The target of a picture opened from a place: a new attachment of `to`'s field, or the attachment `replace`. */
export const useMarkupTarget = (opts: { to?: DraftKey | null; replace?: string } = {}): IMarkupTarget => {
  const to = opts.to ?? undefined;
  const { replace } = opts;
  return useMemo(
    () => ({
      replacing: replace !== undefined,
      give: (file: File) => toComposer({ file, replace, to }),
    }),
    [to, replace],
  );
};
