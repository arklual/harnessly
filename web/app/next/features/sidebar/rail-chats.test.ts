import { describe, expect, it } from 'vitest';
import { activeChats } from '@/next/features/sidebar/rail-chats';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';

const chat = (id: string, at: number, status: ISidebarChat['status']): ISidebarChat => ({
  id,
  title: id,
  status,
  pinned: false,
  projectId: null,
  createdAt: at,
  lastPromptAt: at,
  lastActivityAt: at,
  terminal: true,
  canStopTerminal: true,
});

describe('activeChats', () => {
  it('orders the marks by the last message, whatever the status', () => {
    const list = [
      chat('done', 10, CHAT_STATUS.done),
      chat('asks', 30, CHAT_STATUS.question),
      chat('works', 20, CHAT_STATUS.thinking),
      chat('quiet', 40, CHAT_STATUS.none),
    ];
    expect(activeChats(list).map((c) => c.id)).toEqual(['asks', 'works', 'done']);
  });

  it('does not move a mark when its chat finishes', () => {
    const working = [chat('a', 10, CHAT_STATUS.thinking), chat('b', 20, CHAT_STATUS.thinking)];
    const finished = working.map((c) => (c.id === 'a' ? { ...c, status: CHAT_STATUS.done } : c));
    expect(activeChats(finished).map((c) => c.id)).toEqual(activeChats(working).map((c) => c.id));
  });

  it('moves a chat to the first place when the person writes to it', () => {
    const list = [chat('a', 10, CHAT_STATUS.thinking), chat('b', 20, CHAT_STATUS.thinking)];
    const written = list.map((c) => (c.id === 'a' ? { ...c, lastPromptAt: 99 } : c));
    expect(activeChats(written).map((c) => c.id)).toEqual(['a', 'b']);
  });
});
