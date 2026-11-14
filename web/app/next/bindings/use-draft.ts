// A composer on its draft: the text and files of one field (a chat's, or the new chat's) from the drafts store, the
// writes into it, and the caret at the end of a text that was there when the field came back.
import { useCallback, useLayoutEffect, useRef, type RefObject } from 'react';
import type { IAnnotation } from '@/next/features/pane/annotations';
import type { AttachmentRef, MessageRef } from '@/next/protocol/v2';
import { useSlice } from '@/next/core/store';
import { draftsStore, draftText, setDraftText, useDraft, type DraftKey } from '@/next/stores/drafts';

export interface IDraftField {
  text: string;
  files: AttachmentRef[];
  /** The places attached to the field (a design, a document…). */
  refs: MessageRef[];
  /** The marks sent from the pane's browser. */
  notes: IAnnotation[];
  setText: (value: string) => void;
  /** The text as the store has it now (for a dictation that outlives the field). */
  read: () => string;
  field: RefObject<HTMLTextAreaElement | null>;
}

// One empty list for every field without files: a fresh [] on each keystroke would re-render everything handed it.
const NONE_FILES: AttachmentRef[] = [];
const NONE_REFS: MessageRef[] = [];
const NONE_NOTES: IAnnotation[] = [];

export function useDraftField(key: DraftKey, field?: RefObject<HTMLTextAreaElement | null>): IDraftField {
  const draft = useDraft(key);
  const own = useRef<HTMLTextAreaElement | null>(null);
  const ref = field ?? own;
  const files = draft?.attachments ?? NONE_FILES;
  const refs = draft?.refs ?? NONE_REFS;
  const notes = draft?.annotations ?? NONE_NOTES;
  const setText = useCallback((value: string) => setDraftText(key, value), [key]);
  const read = useCallback(() => draftText(key), [key]);
  // Back in a chat with a draft, the person goes on writing after it, not before it.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || el.value === '' || document.activeElement === el) return;
    el.setSelectionRange(el.value.length, el.value.length);
  }, [key, ref]);
  return { text: draft?.text ?? '', files, refs, notes, setText, read, field: ref };
}

/** The field without its text: what a big host (the chat's dock) needs on each render, and nothing that changes on a key. */
export interface IDraftParts {
  files: AttachmentRef[];
  refs: MessageRef[];
  notes: IAnnotation[];
  /** The field is empty: the starters show under it. */
  empty: boolean;
  /** The text begins with «/»: the apps' commands are read for the palette. */
  slash: boolean;
}

const sameParts = (a: IDraftParts, b: IDraftParts) =>
  a.files === b.files &&
  a.refs === b.refs &&
  a.notes === b.notes &&
  a.empty === b.empty &&
  a.slash === b.slash;

/**
 * A composer whose host is big: the host re-renders when the files, the places, the marks or the field's
 * emptiness change, not on every key. The text itself goes to the field through DraftText (or `read` at the send).
 */
export function useDraftParts(key: DraftKey, field?: RefObject<HTMLTextAreaElement | null>) {
  const parts = useSlice(
    draftsStore,
    key,
    (d): IDraftParts => {
      const text = d?.text ?? '';
      return {
        files: d?.attachments ?? NONE_FILES,
        refs: d?.refs ?? NONE_REFS,
        notes: d?.annotations ?? NONE_NOTES,
        empty: text === '',
        slash: text.trimStart().startsWith('/'),
      };
    },
    sameParts,
  );
  const own = useRef<HTMLTextAreaElement | null>(null);
  const ref = field ?? own;
  const setText = useCallback((value: string) => setDraftText(key, value), [key]);
  const read = useCallback(() => draftText(key), [key]);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || el.value === '' || document.activeElement === el) return;
    el.setSelectionRange(el.value.length, el.value.length);
  }, [key, ref]);
  return { ...parts, setText, read, field: ref };
}

/** The field's text alone, for the one component that shows it (null key: none). */
export const useDraftText = (key: DraftKey | null): string =>
  useSlice(draftsStore, key, (d) => d?.text ?? '');
