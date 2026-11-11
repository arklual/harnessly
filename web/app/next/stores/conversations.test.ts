import { beforeEach, describe, expect, it } from 'vitest';
import { resetCore } from '@/next/core/testing/reset';
import { setScheduler } from '@/next/core/store';
import { byActivity, conversationsByActivity, conversationsStore } from '@/next/stores/conversations';
import type { ConvId, Conversation } from '@/next/protocol/v2';

const conv = (id: string, lastActivityAt: number, more: Partial<Conversation> = {}): Conversation =>
  ({ id, machineId: 'm', title: id, lastActivityAt, pinned: false, ...more }) as unknown as Conversation;

const put = (c: Conversation) => conversationsStore.set(c.id as ConvId, c);
const ids = () => conversationsByActivity().map((c) => c.id);

beforeEach(() => {
  resetCore();
  setScheduler('sync');
});

describe('the chats in the sidebar order', () => {
  it('is the store sorted: pinned first, then by the last activity', () => {
    put(conv('a', 1));
    put(conv('b', 3));
    put(conv('c', 2, { pinned: true }));
    expect(ids()).toEqual(['c', 'b', 'a']);
    expect(conversationsByActivity()).toEqual([...conversationsStore.values()].sort(byActivity));
  });

  it('is one list for every reader until the store changes', () => {
    put(conv('a', 1));
    put(conv('b', 2));
    expect(conversationsByActivity()).toBe(conversationsByActivity());
  });

  it('an event that does not move a chat keeps every other row and the order, without sorting again', () => {
    put(conv('a', 1));
    put(conv('b', 3));
    put(conv('c', 2));
    const before = conversationsByActivity();
    put(conv('c', 2, { title: 'renamed' }));
    const after = conversationsByActivity();
    expect(after).not.toBe(before);
    expect(after.map((c) => c.id)).toEqual(['b', 'c', 'a']);
    expect(after[0]).toBe(before[0]);
    expect(after[2]).toBe(before[2]);
    expect(after[1].title).toBe('renamed');
  });

  it('activity, a pin, a new chat and a removed one put the list in order again', () => {
    put(conv('a', 1));
    put(conv('b', 3));
    put(conv('c', 2));
    expect(ids()).toEqual(['b', 'c', 'a']);
    put(conv('a', 9));
    expect(ids()).toEqual(['a', 'b', 'c']);
    put(conv('c', 2, { pinned: true }));
    expect(ids()).toEqual(['c', 'a', 'b']);
    put(conv('d', 5));
    expect(ids()).toEqual(['c', 'a', 'd', 'b']);
    conversationsStore.delete('a' as ConvId);
    expect(ids()).toEqual(['c', 'd', 'b']);
    // One gone and one come in the same change: the same count, another set.
    conversationsStore.delete('b' as ConvId);
    put(conv('e', 1));
    expect(ids()).toEqual(['c', 'd', 'e']);
  });
});
