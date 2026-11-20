import { describe, expect, it } from 'vitest';
import { NEWS_CLOCK, arrivalToFront, liveNews, tickNews } from '@/next/features/smart-notice/flow';
import { SMART_KIND, type ISmartItem, type TSmartKind } from '@/next/features/smart-notice/smart-notice';

const item = (key: string, kind: TSmartKind): ISmartItem => ({
  key,
  kind,
  chatId: null,
  icon: 'bell',
  title: key,
  text: '',
  at: 0,
  until: null,
});

describe('arrivalToFront', () => {
  it('brings forward what is more urgent than the front plate', () => {
    const front = item('done', SMART_KIND.done);
    const code = item('code', SMART_KIND.code);
    expect(arrivalToFront([item('q', SMART_KIND.question), code], front)).toBe(code);
  });

  it('puts what is as urgent or less behind the front plate', () => {
    const front = item('q1', SMART_KIND.question);
    expect(arrivalToFront([item('q2', SMART_KIND.question)], front)).toBeNull();
    expect(arrivalToFront([item('d', SMART_KIND.done)], front)).toBeNull();
  });

  it('shows the first arrival on an empty plate', () => {
    const q = item('q', SMART_KIND.question);
    expect(arrivalToFront([q], null)).toBe(q);
    expect(arrivalToFront([], null)).toBeNull();
  });
});

describe('tickNews', () => {
  const NOW = 1_000_000;
  const LIFE = 7000;

  it('gives news its life from when it came', () => {
    const clock = tickNews(NEWS_CLOCK, [{ key: 'a', at: NOW - 1000 }], false, NOW, LIFE);
    expect(liveNews(clock, NOW + LIFE - 1001).has('a')).toBe(true);
    expect(liveNews(clock, NOW + LIFE).has('a')).toBe(false);
  });

  it('does not keep news that came from a clock far ahead of this one (a sync from another device)', () => {
    // «at» ten minutes ahead of this page: it used to live for ten minutes and seven seconds.
    const clock = tickNews(NEWS_CLOCK, [{ key: 'a', at: NOW + 600_000 }], false, NOW, LIFE);
    expect(liveNews(clock, NOW + LIFE + 1).has('a')).toBe(false);
  });

  it('shows no news that was old when this page first saw it', () => {
    const clock = tickNews(NEWS_CLOCK, [{ key: 'a', at: NOW - 60_000 }], false, NOW, LIFE);
    expect(liveNews(clock, NOW).has('a')).toBe(false);
  });
});
