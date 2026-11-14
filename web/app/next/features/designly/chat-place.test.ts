// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import {
  chatPlaceOf,
  clearPlain,
  designPlaceOf,
  keepChatPlace,
  MAX_PLACES,
  openPlainOnce,
  parsePlaces,
  resetChatPlaces,
} from '@/next/features/designly/chat-place';
import { ROUTE } from '@/next/routes/routes';

beforeEach(() => {
  localStorage.clear();
  resetChatPlaces();
});

describe('chat-place: a chat that edits a design', () => {
  it('opens as the design in editing, with the chat, on the page it was left on and its machine', () => {
    keepChatPlace('tab-a', { design: 'harnessly', machine: 'm1', page: 'Главная.dc.html' });
    expect(designPlaceOf({ kind: ROUTE.chat, id: 'tab-a' })).toEqual({
      kind: ROUTE.site,
      id: 'harnessly',
      chat: 'tab-a',
      edit: true,
      page: 'Главная.dc.html',
      machine: 'm1',
    });
  });

  it('a chat with no design, and any other place, are shown as they are', () => {
    const chat = { kind: ROUTE.chat, id: 'tab-b' } as const;
    expect(designPlaceOf(chat)).toBe(chat);
    const home = { kind: ROUTE.home } as const;
    expect(designPlaceOf(home)).toBe(home);
  });

  it('«Открыть чат» shows the plain chat once; after it is left the chat is the design again', () => {
    keepChatPlace('tab-a', { design: 'd', machine: null, page: null });
    openPlainOnce('tab-a');
    const chat = { kind: ROUTE.chat, id: 'tab-a' } as const;
    expect(designPlaceOf(chat)).toBe(chat);
    clearPlain('tab-a');
    expect(designPlaceOf(chat)).toBe(chat);
    clearPlain(null);
    expect(designPlaceOf(chat)).toEqual({ kind: ROUTE.site, id: 'd', chat: 'tab-a', edit: true });
  });

  it('is kept in the browser, the oldest beyond the limit forgotten', () => {
    for (let i = 0; i <= MAX_PLACES; i += 1)
      keepChatPlace(`tab-${i}`, { design: 'd', machine: null, page: null });
    const kept = parsePlaces(localStorage.getItem('hx-design-chat-places'));
    expect(kept.size).toBe(MAX_PLACES);
    expect(kept.has('tab-0')).toBe(false);
    resetChatPlaces();
    expect(chatPlaceOf(`tab-${MAX_PLACES}`)).toEqual({ design: 'd', machine: null, page: null });
  });

  it('a broken or foreign value keeps nothing', () => {
    expect(parsePlaces('{').size).toBe(0);
    expect(parsePlaces(JSON.stringify({ 'tab-a': { design: 1 } })).size).toBe(0);
  });
});
