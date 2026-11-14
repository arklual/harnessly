// The field's stash for the composer: the drafts put aside as rows, and the three things the person does with them.
import { useMemo } from 'react';
import type { IStash } from '@/next/features/composer/stash-list';
import { useSlice } from '@/next/core/store';
import { draftsStore, type DraftKey, emptyDraft } from '@/next/stores/drafts';
import { dropStash, restoreStash, stashDraft, useStash } from '@/next/stores/stash';

/** The first line of a draft as its row's name. */
const previewOf = (text: string): string => text.trim().split('\n')[0].slice(0, 120);

export function useFieldStash(key: DraftKey): IStash {
  const items = useStash(key);
  // Only whether there is anything to put aside: the whole draft would re-render the chat's dock on every key.
  const canStash = useSlice(draftsStore, key, (draft) => !!draft && !emptyDraft(draft));
  return useMemo(
    () => ({
      rows: items.map((i) => ({
        id: i.id,
        preview: previewOf(i.text),
        files: i.attachments.length,
        at: i.at,
      })),
      canStash,
      onStash: () => void stashDraft(key),
      onRestore: (id) => restoreStash(key, id),
      onDrop: (id) => dropStash(key, id),
    }),
    [items, canStash, key],
  );
}
