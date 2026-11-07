import { describe, expect, it } from 'vitest';
import { hasTestChats, withoutTestChats } from '@/next/features/sidebar/test-chats';
import type { ISidebarChat } from '@/next/features/sidebar/types';

const chat = (id: string, test = false): ISidebarChat =>
  ({ id, title: id, test: test || undefined, pinned: false }) as ISidebarChat;

describe('test chats (HRN-842)', () => {
  const chats = [chat('a'), chat('t1', true), chat('t2', true)];

  it('leaves the test chats out by default', () => {
    expect(withoutTestChats(chats, false, null).map((c) => c.id)).toEqual(['a']);
  });

  it('keeps the open test chat in view', () => {
    expect(withoutTestChats(chats, false, 't2').map((c) => c.id)).toEqual(['a', 't2']);
  });

  it('shows them all when asked, and hands back the same list when there are none', () => {
    expect(withoutTestChats(chats, true, null)).toBe(chats);
    const plain = [chat('a')];
    expect(withoutTestChats(plain, false, null)).toBe(plain);
  });

  it('says whether any chat is a test chat', () => {
    expect(hasTestChats(chats)).toBe(true);
    expect(hasTestChats([chat('a')])).toBe(false);
    expect(hasTestChats(null)).toBe(false);
  });
});
