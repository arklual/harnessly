import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  atPhrase,
  leftText,
  limitLine,
  limitPhase,
  limitPlaceholder,
  limitTitle,
  LIMIT_PHASE,
} from '@/next/features/chat/limit';
import type { LimitHold } from '@/next/protocol/v2';

const MIN = 60_000;
const HOUR = 60 * MIN;
const at = (day: number, h: number, m = 0) => new Date(2026, 9, day, h, m).getTime();
// Sunday 25 October 2026, a quarter to four in the afternoon.
const now = at(25, 15, 45);

const hold = (patch: Partial<LimitHold> = {}): LimitHold => ({
  provider: 'claude',
  window: 'session',
  at: now - 5 * MIN,
  resetsAt: at(25, 18),
  continueAt: at(25, 18),
  ...patch,
});

beforeAll(() => pinLocale('ru'));
afterAll(() => pinLocale(null));

describe('the phase of a hold', () => {
  it('goes on by itself when the runtime has a time to go on', () =>
    expect(limitPhase(hold())).toBe(LIMIT_PHASE.auto));
  it('waits for the person when it will not go on by itself but knows the reset', () =>
    expect(limitPhase(hold({ continueAt: null }))).toBe(LIMIT_PHASE.held));
  it('has nothing to say about time when the reset is unknown', () =>
    expect(limitPhase(hold({ continueAt: null, resetsAt: null }))).toBe(LIMIT_PHASE.unknown));
});

describe('what is left', () => {
  it('counts hours and minutes, rounded up to the minute', () => {
    expect(leftText(2 * HOUR + 14 * MIN)).toBe('2 ч 14 мин');
    expect(leftText(2 * HOUR + 13 * MIN + 1)).toBe('2 ч 14 мин');
    expect(leftText(14 * MIN)).toBe('14 мин');
  });
  it('is never less than a minute, even when the time has come', () => {
    expect(leftText(5_000)).toBe('1 мин');
    expect(leftText(-HOUR)).toBe('1 мин');
  });
  it('counts days for a weekly reset', () => expect(leftText(3 * 24 * HOUR + 2 * HOUR)).toBe('3 д 2 ч'));
});

describe('a moment ahead', () => {
  it('is a clock time today and a weekday within the week', () => {
    expect(atPhrase(at(25, 18), now)).toBe('в 18:00');
    expect(atPhrase(at(27, 9), now)).toMatch(/^в \S+ 09:00$/);
  });
  it('is a date later than a week, without «в»', () => {
    expect(atPhrase(at(12, 9) + 31 * 24 * HOUR, now)).toMatch(/^\d+ \S+, 09:00$/);
  });
});

describe('the card line', () => {
  it('names the time the agent goes on and how long that is', () =>
    expect(limitLine(hold(), now)).toBe(
      'Лимит Claude исчерпан · продолжу автоматически в 18:00 (через 2 ч 15 мин)',
    ));
  it('says only until when when it will not go on by itself', () =>
    expect(limitLine(hold({ continueAt: null }), now)).toBe('Лимит Claude исчерпан до 18:00'));
  it('says only that the limit is out when no time is known', () =>
    expect(limitLine(hold({ continueAt: null, resetsAt: null }), now)).toBe('Лимит Claude исчерпан'));
  it('names the agent and the weekly window', () =>
    expect(limitTitle(hold({ provider: 'codex', window: 'weekly' }))).toBe('Недельный лимит Codex исчерпан'));
  it('speaks English too', () => {
    pinLocale('en');
    expect(limitTitle(hold())).toBe('Claude limit reached');
    pinLocale('ru');
  });
});

describe('the hint in the field', () => {
  it('says when a message written now goes', () =>
    expect(limitPlaceholder(hold(), now)).toBe('Отправится после сброса лимита в 18:00'));
  it('says it without a time when the reset is unknown', () =>
    expect(limitPlaceholder({ resetsAt: null }, now)).toBe('Отправится после сброса лимита'));
});
