import { describe, expect, it } from 'vitest';
import {
  isIdle,
  isUntouched,
  raiseIndex,
  RANK,
  rankOf,
  TAB_IDLE_MS,
  TAB_PLACE,
  type ITidyTab,
  type TRank,
} from '@/next/features/shell/tab-tidy';
import { CHAT_STATUS } from '@/next/features/sidebar/types';

const chat = (patch: Partial<ITidyTab> = {}): ITidyTab => ({
  id: 'a',
  place: TAB_PLACE.chat,
  status: CHAT_STATUS.none,
  empty: false,
  draft: false,
  leftAt: 0,
  activityAt: 0,
  ...patch,
});

describe('isUntouched: a tab nothing was done in goes when left', () => {
  it('closes a start page and a chat without a message', () => {
    expect(isUntouched(chat({ place: TAB_PLACE.home, status: null }))).toBe(true);
    expect(isUntouched(chat({ empty: true }))).toBe(true);
  });

  it('keeps a chat with a message, a draft, an unknown status, and a page that is not a chat', () => {
    expect(isUntouched(chat())).toBe(false);
    expect(isUntouched(chat({ empty: true, draft: true }))).toBe(false);
    expect(isUntouched(chat({ empty: true, status: null }))).toBe(false);
    expect(isUntouched(chat({ empty: true, status: CHAT_STATUS.thinking }))).toBe(false);
    expect(isUntouched(chat({ place: TAB_PLACE.other, status: null }))).toBe(false);
  });
});

describe('isIdle: a tab left alone for long goes', () => {
  const late = TAB_IDLE_MS + 1;

  it('closes a quiet chat and any other page after the idle time', () => {
    expect(isIdle(chat(), late)).toBe(true);
    expect(isIdle(chat({ place: TAB_PLACE.other, status: null, activityAt: null }), late)).toBe(true);
    expect(isIdle(chat(), TAB_IDLE_MS - 1)).toBe(false);
  });

  it('counts from the chat’s last activity when it is later than the leave', () => {
    expect(isIdle(chat({ activityAt: 10 }), late)).toBe(false);
  });

  it('keeps a chat at work, waiting for an answer, finished and not read, with a draft, or not known yet', () => {
    for (const status of [
      CHAT_STATUS.thinking,
      CHAT_STATUS.background,
      CHAT_STATUS.question,
      CHAT_STATUS.perm,
      CHAT_STATUS.plan,
      CHAT_STATUS.done,
      CHAT_STATUS.failed,
      CHAT_STATUS.limited,
    ]) {
      expect(isIdle(chat({ status }), late)).toBe(false);
    }
    expect(isIdle(chat({ draft: true }), late)).toBe(false);
    expect(isIdle(chat({ status: null }), late)).toBe(false);
  });

  it('keeps the active tab (it has no leave)', () => {
    expect(isIdle(chat({ leftAt: null }), late)).toBe(false);
  });
});

describe('raiseIndex: a tab that comes to want the person moves to the front', () => {
  const ranks = (entries: [string, TRank][]) => new Map(entries);

  it('puts a question first and a finished turn behind the questions', () => {
    const order = ['a', 'b', 'c', 'd'];
    expect(raiseIndex(order, ranks([['d', RANK.answer]]), 'd')).toBe(0);
    expect(
      raiseIndex(
        order,
        ranks([
          ['a', RANK.answer],
          ['d', RANK.read],
        ]),
        'd',
      ),
    ).toBe(1);
    expect(
      raiseIndex(
        order,
        ranks([
          ['a', RANK.read],
          ['d', RANK.answer],
        ]),
        'd',
      ),
    ).toBe(0);
  });

  it('leaves a tab that is there already or that wants nothing', () => {
    expect(raiseIndex(['a', 'b'], ranks([['a', RANK.answer]]), 'a')).toBeNull();
    expect(raiseIndex(['a', 'b'], ranks([]), 'b')).toBeNull();
  });

  it('ranks by status', () => {
    expect(rankOf(CHAT_STATUS.perm)).toBe(RANK.answer);
    expect(rankOf(CHAT_STATUS.done)).toBe(RANK.read);
    expect(rankOf(CHAT_STATUS.thinking)).toBe(RANK.none);
    expect(rankOf(null)).toBe(RANK.none);
  });
});
