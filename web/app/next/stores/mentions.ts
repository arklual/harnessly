// What the person took from the Apps screen to talk about: a task or a merge request. It sits as a chip in the start
// page's field until the message goes, and then leads its text as a line with the link, so the agent can open it. Kept
// for the page only, like the quotes.

import { createKeyedStore, flush, useKey } from '@/next/core/store';
import type { DraftKey } from '@/next/stores/drafts';

export const MENTION_KIND = { task: 'task', mr: 'mr' } as const;
export type TMentionKind = (typeof MENTION_KIND)[keyof typeof MENTION_KIND];

export interface Mention {
  /** Tells it from the others of the field: the key of the row it was taken from. */
  id: string;
  kind: TMentionKind;
  /** What the chip says: `ABC-2628 · Title`. */
  label: string;
  /** Where it is, for the agent. */
  url: string;
}

const NONE: readonly Mention[] = [];

export const mentionsStore = createKeyedStore<DraftKey, readonly Mention[]>();

export const useMentions = (k: DraftKey | null): readonly Mention[] => useKey(mentionsStore, k) ?? NONE;

/** The chips of the field are these, whatever were there. */
export function setMentions(k: DraftKey, list: readonly Mention[]): void {
  if (list.length === 0) mentionsStore.delete(k);
  else mentionsStore.set(k, list);
  flush();
}

export function removeMention(k: DraftKey, id: string): void {
  mentionsStore.patch(k, (have) => {
    const left = (have ?? NONE).filter((m) => m.id !== id);
    return left.length > 0 ? left : undefined;
  });
}

export const clearMentions = (k: DraftKey): void => mentionsStore.delete(k);

/** The message as it goes: a line per mention with its link, then the person's text. */
export const withMentions = (mentions: readonly Mention[], text: string): string => {
  const lines = mentions.map((m) => `${m.label} — ${m.url}`).join('\n');
  if (!text) return lines;
  return lines ? `${lines}\n\n${text}` : text;
};
