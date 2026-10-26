// A composer on its draft: the text and files of one field (a chat's, or the new chat's) from the drafts store, the
// writes into it, and the caret at the end of a text that was there when the field came back.
import { useCallback, useLayoutEffect, useRef, type RefObject } from 'react';
import type { AttachmentRef, MessageRef } from '@/next/protocol/v2';
import { draftText, setDraftText, useDraft, type DraftKey } from '@/next/stores/drafts';

export interface IDraftField {
  text: string;
  files: AttachmentRef[];
  /** The places attached to the field (a design, a document…). */
  refs: MessageRef[];
  setText: (value: string) => void;
  /** The text as the store has it now (for a dictation that outlives the field). */
  read: () => string;
  field: RefObject<HTMLTextAreaElement | null>;
}

// One empty list for every field without files: a fresh [] on each keystroke would re-render everything handed it.
const NONE_FILES: AttachmentRef[] = [];
const NONE_REFS: MessageRef[] = [];

export function useDraftField(key: DraftKey, field?: RefObject<HTMLTextAreaElement | null>): IDraftField {
  const draft = useDraft(key);
  const own = useRef<HTMLTextAreaElement | null>(null);
  const ref = field ?? own;
  const files = draft?.attachments ?? NONE_FILES;
  const refs = draft?.refs ?? NONE_REFS;
  const setText = useCallback((value: string) => setDraftText(key, value), [key]);
  const read = useCallback(() => draftText(key), [key]);
  // Back in a chat with a draft, the person goes on writing after it, not before it.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || el.value === '' || document.activeElement === el) return;
    el.setSelectionRange(el.value.length, el.value.length);
  }, [key, ref]);
  return { text: draft?.text ?? '', files, refs, setText, read, field: ref };
}
