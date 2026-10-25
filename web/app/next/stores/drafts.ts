// The composer's draft per conversation: text and the attachments already uploaded. Written to IndexedDB 300 ms
// after the last keystroke (and when the page is hidden), so a chat switch, a reload or a lost connection keeps it;
// the text also goes to the machine for the person's other devices (core/drafts-cloud).

import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { AttachmentRef, ConvId, MessageRef } from '@/next/protocol/v2';

export interface Draft {
  text: string;
  attachments: AttachmentRef[];
  /** The places the field points at (a design, a document, a board, a file, an MR, a task); none when absent. */
  refs?: MessageRef[];
}

const NO_REFS: MessageRef[] = [];

/** A draft with nothing in it: no text, no files, no references. */
export const emptyDraft = (d: Draft): boolean =>
  d.text === '' && d.attachments.length === 0 && (d.refs ?? NO_REFS).length === 0;

/** The key of a new chat's draft before it has a conversation. */
export const NEW_CHAT = 'new' as const;
/** The draft of a design that has no chat yet: its first message (bindings/design-conv.ts). */
export type DesignDraftKey = `design:${string}`;
export type DraftKey = ConvId | typeof NEW_CHAT | DesignDraftKey;

export const draftsStore = createKeyedStore<DraftKey, Draft>();

export const useDraft = (k: DraftKey | null): Draft | undefined => useKey(draftsStore, k);

/**
 * The field is controlled by this store, so a keystroke must reach it before the event ends: a frame later the field
 * would show the old text for a moment, and a key pressed in that moment (Enter right after a paste) would act on it.
 */
export function setDraft(k: DraftKey, d: Draft): void {
  if (emptyDraft(d)) draftsStore.delete(k);
  else if (d.refs && d.refs.length === 0) draftsStore.set(k, { text: d.text, attachments: d.attachments });
  else draftsStore.set(k, d);
  flush();
}

export const clearDraft = (k: DraftKey): void => draftsStore.delete(k);

/** The field's text as it is now; '' for a field without a draft. */
export const draftText = (k: DraftKey): string => draftsStore.get(k)?.text ?? '';

/**
 * Only the text: the attachments are read when the write happens, so a write that comes late (the end of a dictation
 * after the person left the chat and added a file there) does not take them back.
 */
export function setDraftText(k: DraftKey, text: string): void {
  const have = draftsStore.get(k);
  setDraft(k, { ...have, text, attachments: have?.attachments ?? [] });
}

/** The field's references as they are now. */
export const draftRefs = (k: DraftKey): MessageRef[] => draftsStore.get(k)?.refs ?? NO_REFS;

const refKey = (r: MessageRef): string => `${r.kind}:${r.id}`;

/** One more place in the field; the same place again only moves to the end. At most `max`. */
export function addDraftRef(k: DraftKey, ref: MessageRef, max: number): void {
  const have = draftsStore.get(k);
  const refs = [...(have?.refs ?? NO_REFS).filter((r) => refKey(r) !== refKey(ref)), ref].slice(-max);
  setDraft(k, { text: have?.text ?? '', attachments: have?.attachments ?? [], refs });
}

export function removeDraftRef(k: DraftKey, ref: Pick<MessageRef, 'kind' | 'id'>): void {
  const have = draftsStore.get(k);
  if (!have?.refs) return;
  const refs = have.refs.filter((r) => refKey(r) !== refKey(ref as MessageRef));
  setDraft(k, { ...have, refs });
}
