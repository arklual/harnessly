import { describe, expect, it } from 'vitest';
import { nextActiveChat } from '@/next/features/chat/next-chat-pull';
import { CHAT_STATUS, type ISidebarChat, type TChatStatus } from '@/next/features/sidebar/types';

const chat = (id: string, lastPromptAt: number, status: TChatStatus, pinned = false): ISidebarChat => ({
  id,
  title: id,
  status,
  pinned,
  projectId: null,
  createdAt: lastPromptAt,
  lastPromptAt,
  lastActivityAt: lastPromptAt,
  terminal: true,
  canStopTerminal: false,
});

describe('the next active chat', () => {
  it('is the next one at work or unread in the sidebar order, going round to the top', () => {
    const list = [
      chat('a', 50, CHAT_STATUS.thinking),
      chat('b', 40, CHAT_STATUS.none),
      chat('c', 30, CHAT_STATUS.done),
      chat('d', 20, CHAT_STATUS.limited),
      chat('p', 1, CHAT_STATUS.question, true),
    ];
    expect(nextActiveChat(list, 'a')?.id).toBe('c');
    expect(nextActiveChat(list, 'c')?.id).toBe('p');
    expect(nextActiveChat(list, 'p')?.id).toBe('a');
  });

  it('is none when no other chat is active', () => {
    const list = [chat('a', 2, CHAT_STATUS.thinking), chat('b', 1, CHAT_STATUS.none)];
    expect(nextActiveChat(list, 'a')).toBeNull();
  });
});
