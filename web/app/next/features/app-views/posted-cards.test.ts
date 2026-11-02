// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import {
  CARD_DATA_MAX,
  CARDS_MAX,
  parseCards,
  postCard,
  resetCards,
} from '@/next/features/app-views/posted-cards';

const kept = (chat: string) => parseCards(localStorage.getItem('hx-app-cards')).get(chat) ?? [];

beforeEach(() => {
  localStorage.clear();
  resetCards();
});
afterEach(() => {
  localStorage.clear();
  resetCards();
});

describe('postCard', () => {
  it('keeps a card with its chat, and the chats apart', () => {
    expect(postCard('c1', { app: 'a.one', view: 'card', data: { n: 1 } })).toBe(true);
    expect(postCard('c2', { app: 'a.one', view: 'card', data: null })).toBe(true);
    expect(kept('c1')).toHaveLength(1);
    expect(kept('c1')[0]).toMatchObject({ app: 'a.one', view: 'card', data: { n: 1 } });
    expect(kept('c2')).toHaveLength(1);
  });

  it('refuses data too big to be a card', () => {
    expect(postCard('c1', { app: 'a.one', view: 'card', data: 'x'.repeat(CARD_DATA_MAX + 1) })).toBe(false);
    expect(kept('c1')).toHaveLength(0);
  });

  it('keeps the newest cards only', () => {
    for (let i = 0; i < CARDS_MAX + 3; i += 1) postCard('c1', { app: 'a.one', view: 'card', data: i });
    const cards = kept('c1');
    expect(cards).toHaveLength(CARDS_MAX);
    expect(cards[0]?.data).toBe(3);
    expect(cards.at(-1)?.data).toBe(CARDS_MAX + 2);
  });
});

describe('parseCards', () => {
  it('reads nothing from what is not cards', () => {
    expect(parseCards(null).size).toBe(0);
    expect(parseCards('nope').size).toBe(0);
    expect(parseCards('{"c1":[{"id":1}]}').get('c1')).toEqual([]);
  });
});
