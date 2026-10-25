import { afterEach, describe, expect, it } from 'vitest';
import {
  addDraftRef,
  clearDraft,
  draftRefs,
  draftsStore,
  removeDraftRef,
  setDraftText,
} from '@/next/stores/drafts';
import { fieldOf, sameField } from '@/next/stores/draft-sync';
import type { MessageRef } from '@/next/protocol/v2';

const k = 'tab-1' as const;
const ref = (id: string): MessageRef => ({ kind: 'file', id, title: id, target: id });

afterEach(() => clearDraft(k));

describe('the field references', () => {
  it('adds, moves a repeat to the end, keeps at most max, and keeps them through typing', () => {
    addDraftRef(k, ref('a'), 2);
    addDraftRef(k, ref('b'), 2);
    addDraftRef(k, ref('a'), 2);
    expect(draftRefs(k).map((r) => r.id)).toEqual(['b', 'a']);
    addDraftRef(k, ref('c'), 2);
    expect(draftRefs(k).map((r) => r.id)).toEqual(['a', 'c']);
    setDraftText(k, 'hello');
    expect(draftRefs(k).map((r) => r.id)).toEqual(['a', 'c']);
  });

  it('removes one, and a field with nothing left is no draft', () => {
    addDraftRef(k, ref('a'), 10);
    removeDraftRef(k, { kind: 'file', id: 'a' });
    expect(draftsStore.get(k)).toBeUndefined();
  });
});

describe('the synced field with references', () => {
  it('tells fields apart by their references', () => {
    const a = fieldOf({ text: 'x', attachments: [], refs: [ref('a')] });
    expect(sameField(a, fieldOf({ text: 'x', attachments: [], refs: [ref('a')] }))).toBe(true);
    expect(sameField(a, fieldOf({ text: 'x', attachments: [] }))).toBe(false);
    expect(fieldOf({ text: 'x', attachments: [], refs: [] })).toEqual({ text: 'x', attachments: [] });
  });
});
