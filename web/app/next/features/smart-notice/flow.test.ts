import { describe, expect, it } from 'vitest';
import { arrivalToFront } from '@/next/features/smart-notice/flow';
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
