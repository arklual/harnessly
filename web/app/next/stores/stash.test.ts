import { beforeEach, describe, expect, it } from 'vitest';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { dropStash, restoreStash, STASH_MAX, stashDraft, stashStore } from '@/next/stores/stash';

const file = (id: string) => ({ id, name: `${id}.png`, mime: 'image/png', kind: 'image' as const });
const KEY = 'new' as const;

beforeEach(() => {
  for (const k of [...draftsStore.keys()]) draftsStore.delete(k);
  for (const k of [...stashStore.keys()]) stashStore.delete(k);
});

describe('stash', () => {
  it('puts a draft aside with its files and clears the field', () => {
    setDraft(KEY, { text: 'first', attachments: [file('a')] });
    expect(stashDraft(KEY, 1)).toBe(true);
    expect(draftsStore.get(KEY)).toBeUndefined();
    expect(stashStore.get(KEY)?.[0]).toMatchObject({ text: 'first', attachments: [file('a')] });
  });

  it('puts nothing aside from an empty field', () => {
    expect(stashDraft(KEY)).toBe(false);
    setDraft(KEY, { text: '', attachments: [] });
    expect(stashDraft(KEY)).toBe(false);
    expect(stashStore.get(KEY)).toBeUndefined();
  });

  it('brings a draft back after what the field holds, without doubling a file', () => {
    setDraft(KEY, { text: 'old', attachments: [file('a')] });
    stashDraft(KEY, 1);
    setDraft(KEY, { text: 'now', attachments: [file('a'), file('b')] });
    const id = stashStore.get(KEY)?.[0].id ?? '';
    restoreStash(KEY, id);
    expect(draftsStore.get(KEY)?.text).toBe('now\n\nold');
    expect(draftsStore.get(KEY)?.attachments.map((f) => f.id)).toEqual(['a', 'b']);
    expect(stashStore.get(KEY)).toBeUndefined();
  });

  it('forgets one draft and keeps the newest ones when there are too many', () => {
    for (let i = 0; i < STASH_MAX + 3; i += 1) {
      setDraft(KEY, { text: `d${i}`, attachments: [] });
      stashDraft(KEY, i);
    }
    const items = stashStore.get(KEY) ?? [];
    expect(items).toHaveLength(STASH_MAX);
    expect(items[0].text).toBe(`d${STASH_MAX + 2}`);
    dropStash(KEY, items[0].id);
    expect(stashStore.get(KEY)).toHaveLength(STASH_MAX - 1);
  });
});
