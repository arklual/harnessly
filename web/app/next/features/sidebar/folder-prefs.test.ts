import { describe, expect, it } from 'vitest';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';
import {
  FOLDER_DEFAULTS,
  parseFolderPrefs,
  peekedChats,
  PEEK_MAX,
} from '@/next/features/sidebar/folder-prefs';

const NOON = new Date(2026, 10, 18, 12, 0, 0).getTime();
const HOUR = 3_600_000;

const chat = (id: string, status: ISidebarChat['status'], at: number): ISidebarChat =>
  ({
    id,
    title: id,
    status,
    pinned: false,
    projectId: 'p',
    createdAt: at,
    lastPromptAt: at,
    lastActivityAt: at,
  }) as ISidebarChat;

describe('parseFolderPrefs', () => {
  it('falls back to the defaults', () => {
    expect(parseFolderPrefs(null)).toEqual(FOLDER_DEFAULTS);
    expect(parseFolderPrefs('nope')).toEqual(FOLDER_DEFAULTS);
    expect(parseFolderPrefs('{"rows":0,"peek":"x"}')).toEqual(FOLDER_DEFAULTS);
  });
  it('keeps what fits', () => {
    expect(parseFolderPrefs('{"rows":8,"peek":false,"suggest":false}')).toEqual({
      rows: 8,
      peek: false,
      suggest: false,
    });
  });
});

describe('peekedChats', () => {
  it('shows live chats of today and leaves old and quiet ones', () => {
    const list = [
      chat('work', CHAT_STATUS.thinking, NOON - HOUR),
      chat('look', CHAT_STATUS.done, NOON - 2 * HOUR),
      chat('quiet', CHAT_STATUS.none, NOON - HOUR),
      chat('old', CHAT_STATUS.done, NOON - 30 * HOUR),
    ];
    expect(peekedChats(list, NOON).map((c) => c.id)).toEqual(['work', 'look']);
  });
  it('is no longer than PEEK_MAX', () => {
    const list = Array.from({ length: PEEK_MAX + 3 }, (_, i) => chat(`c${i}`, CHAT_STATUS.done, NOON - HOUR));
    expect(peekedChats(list, NOON)).toHaveLength(PEEK_MAX);
  });
  it('always shows the open chat, quiet or old, in its place', () => {
    const list = [
      chat('work', CHAT_STATUS.thinking, NOON - HOUR),
      chat('open', CHAT_STATUS.none, NOON - 30 * HOUR),
      chat('quiet', CHAT_STATUS.none, NOON - HOUR),
    ];
    expect(peekedChats(list, NOON, 'open').map((c) => c.id)).toEqual(['work', 'open']);
  });
  it('keeps the open chat within PEEK_MAX', () => {
    const live = Array.from({ length: PEEK_MAX + 2 }, (_, i) => chat(`c${i}`, CHAT_STATUS.done, NOON - HOUR));
    const list = [...live, chat('open', CHAT_STATUS.none, NOON - 30 * HOUR)];
    const ids = peekedChats(list, NOON, 'open').map((c) => c.id);
    expect(ids).toHaveLength(PEEK_MAX);
    expect(ids).toContain('open');
  });
});
