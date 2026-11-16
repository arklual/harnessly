// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import {
  chatPlaceOf,
  chooseChat,
  designPlaceOf,
  keepChatPlace,
  MAX_PLACES,
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

  it("«К чату» is the chat's choice: after another chat and a reload it stays a plain chat (HRN-1557)", () => {
    keepChatPlace('tab-a', { design: 'd', machine: null, page: null });
    keepChatPlace('tab-b', { design: 'e', machine: null, page: null });
    chooseChat('tab-a');
    const a = { kind: ROUTE.chat, id: 'tab-a' } as const;
    const b = { kind: ROUTE.chat, id: 'tab-b' } as const;
    expect(designPlaceOf(a)).toBe(a);
    // Another chat keeps its own view: B is still its design.
    expect(designPlaceOf(b)).toEqual({ kind: ROUTE.site, id: 'e', chat: 'tab-b', edit: true });
    // Back to A, and after a reload of the page.
    expect(designPlaceOf(a)).toBe(a);
    resetChatPlaces();
    expect(designPlaceOf(a)).toBe(a);
    expect(designPlaceOf(b)).toEqual({ kind: ROUTE.site, id: 'e', chat: 'tab-b', edit: true });
  });

  it('the design opened with the chat again (by the person or the agent) makes it the design again', () => {
    keepChatPlace('tab-a', { design: 'd', machine: null, page: 'p.html' });
    chooseChat('tab-a');
    keepChatPlace('tab-a', { design: 'd', machine: null, page: 'p.html' });
    expect(designPlaceOf({ kind: ROUTE.chat, id: 'tab-a' })).toEqual({
      kind: ROUTE.site,
      id: 'd',
      chat: 'tab-a',
      edit: true,
      page: 'p.html',
    });
  });

  it('«К чату» of a chat with no design keeps nothing', () => {
    chooseChat('tab-x');
    expect(chatPlaceOf('tab-x')).toBeNull();
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
