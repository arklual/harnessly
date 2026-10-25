// «Отложенные»: drafts a person put aside to write something else first, per field (a chat's, or the new chat's). A
// stashed draft keeps its text, files and attached places; taking it back puts them after what the field holds now.
// Kept in IndexedDB next to the drafts (core/persist), so a reload or another visit brings them back. Per browser: they
// do not travel to the person's other devices.
import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { AttachmentRef, MessageRef } from '@/next/protocol/v2';
import { REFS_MAX } from '@/next/protocol/v2';
import { clearDraft, draftsStore, emptyDraft, setDraft, type DraftKey } from '@/next/stores/drafts';

export interface StashItem {
  id: string;
  at: number;
  text: string;
  attachments: AttachmentRef[];
  refs?: MessageRef[];
}

/** A field keeps this many stashed drafts; the oldest goes when one more is put aside. */
export const STASH_MAX = 20;

export const stashStore = createKeyedStore<DraftKey, StashItem[]>();

const NONE: StashItem[] = [];

export const useStash = (k: DraftKey | null): StashItem[] => useKey(stashStore, k) ?? NONE;

const write = (k: DraftKey, items: StashItem[]): void => {
  if (items.length === 0) stashStore.delete(k);
  else stashStore.set(k, items);
  flush();
};

/** Puts the field's draft aside and clears the field; false when there was nothing to put aside. */
export function stashDraft(k: DraftKey, now: number = Date.now()): boolean {
  const have = draftsStore.get(k);
  if (!have || emptyDraft(have)) return false;
  const item: StashItem = {
    id: `${now.toString(36)}-${Math.random().toString(36).slice(2, 7)}`,
    at: now,
    text: have.text,
    attachments: have.attachments,
    ...(have.refs && have.refs.length > 0 ? { refs: have.refs } : {}),
  };
  write(k, [item, ...(stashStore.get(k) ?? NONE)].slice(0, STASH_MAX));
  clearDraft(k);
  flush();
  return true;
}

const refKey = (r: MessageRef): string => `${r.kind}:${r.id}`;

/** Brings a stashed draft back after what the field holds; it leaves the stash. */
export function restoreStash(k: DraftKey, id: string): void {
  const items = stashStore.get(k) ?? NONE;
  const item = items.find((i) => i.id === id);
  if (!item) return;
  const have = draftsStore.get(k);
  const files = new Set((have?.attachments ?? []).map((f) => f.id));
  const refs = have?.refs ?? [];
  const seen = new Set(refs.map(refKey));
  setDraft(k, {
    text: [have?.text ?? '', item.text].filter((s) => s.trim() !== '').join('\n\n'),
    attachments: [...(have?.attachments ?? []), ...item.attachments.filter((f) => !files.has(f.id))],
    refs: [...refs, ...(item.refs ?? []).filter((r) => !seen.has(refKey(r)))].slice(0, REFS_MAX),
  });
  write(
    k,
    items.filter((i) => i.id !== id),
  );
}

export function dropStash(k: DraftKey, id: string): void {
  write(
    k,
    (stashStore.get(k) ?? NONE).filter((i) => i.id !== id),
  );
}
