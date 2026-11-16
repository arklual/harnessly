import { describe, expect, it } from 'vitest';
import { stripTabOf } from '@/next/bindings/tabs';
import { PACE } from '@/next/features/shell/tab-strip';
import { CHAT_STATUS, type ISidebarChat } from '@/next/features/sidebar/types';
import { createTab } from '@/next/stores/tabs';

const chats = (status: string) =>
  new Map([['x', { id: 'x', title: 'Доступ к dev-машине', status } as unknown as ISidebarChat]]);
const chatTab = createTab('/c/x');

describe('stripTabOf: how a chat at work shows', () => {
  it('turns the icon quickly while the agent works and slowly while only its background agents run', () => {
    expect(stripTabOf(chatTab, chats(CHAT_STATUS.thinking)).busy).toBe(PACE.working);
    expect(stripTabOf(chatTab, chats(CHAT_STATUS.background)).busy).toBe(PACE.background);
  });

  it('keeps the icon still for every other status and for a place that is not a chat', () => {
    expect(stripTabOf(chatTab, chats(CHAT_STATUS.question)).busy).toBeNull();
    expect(stripTabOf(chatTab, chats(CHAT_STATUS.none)).busy).toBeNull();
    expect(stripTabOf(createTab('/settings'), new Map()).busy).toBeNull();
  });

  it('adds what a chat at work in the background waits for to the hint of its tab', () => {
    const waiting = new Map([
      [
        'x',
        {
          id: 'x',
          title: 'Чат',
          status: CHAT_STATUS.background,
          waitsFor: 'Ждёт: команда npm test',
        } as ISidebarChat,
      ],
    ]);
    const tab = stripTabOf(chatTab, waiting);
    expect(tab.busy).toBe(PACE.background);
    expect(tab.note).toBe('Ждёт: команда npm test');
    expect(stripTabOf(chatTab, chats(CHAT_STATUS.thinking)).note).toBeUndefined();
  });

  it('shows the whole title of the chat', () => {
    expect(stripTabOf(chatTab, chats(CHAT_STATUS.none)).title).toBe('Доступ к dev-машине');
  });
});

describe('stripTabOf: a chat tab is named by the chat (HRN-1557)', () => {
  const named = (title: string) =>
    new Map([['x', { id: 'x', title, status: CHAT_STATUS.none } as unknown as ISidebarChat]]);
  const design = { ...createTab('/designly/harnessly?chat=x&edit=1'), title: 'harnessly' };

  it("a design edited with its chat shows the chat's name, not the design's", () => {
    expect(stripTabOf(design, named('Редизайн досок')).title).toBe('Редизайн досок');
  });

  it('follows a rename of the chat', () => {
    expect(stripTabOf(design, named('Новое имя')).title).toBe('Новое имя');
    expect(stripTabOf(chatTab, named('Новое имя')).title).toBe('Новое имя');
  });

  it('a chat with no title yet is «New chat», not the design', () => {
    expect(stripTabOf({ ...design, title: null }, named('')).title).toBe('New chat');
    expect(stripTabOf({ ...chatTab, title: null }, new Map()).title).toBe('New chat');
  });

  it("a design looked at without a chat keeps the design's name", () => {
    expect(stripTabOf({ ...createTab('/designly/harnessly'), title: 'harnessly' }, new Map()).title).toBe(
      'harnessly',
    );
  });
});
