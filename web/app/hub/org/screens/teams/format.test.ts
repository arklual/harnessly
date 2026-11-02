import { describe as suite, expect, it } from 'vitest';

import { ago, describe, levelOf, needsText, ofLimit, people, sharePct, thousand } from './format';

suite('teams formats', () => {
  it('writes money in thousands', () => {
    expect(thousand(17_500)).toBe('17,5 тыс');
    expect(thousand(120_000)).toBe('120 тыс');
    expect(ofLimit(17_500, 20_000)).toBe('17,5 тыс из 20 тыс ₽');
    expect(ofLimit(800, null)).toBe('0,8 тыс ₽');
  });
  it('levels by share of the limit', () => {
    expect(sharePct(110_000, 120_000)).toBe(92);
    expect(sharePct(5, null)).toBeNull();
    expect([79, 80, 89, 90].map((n) => levelOf(n))).toEqual(['ok', 'warn', 'warn', 'bad']);
  });
  it('names who may change what', () => {
    expect(needsText('tenant_admin')).toBe('Может поменять: админ тенанта');
    expect(people(22)).toBe('22 человека');
  });
  it('says how long ago', () => {
    const now = Date.parse('2026-10-31T12:00:00Z');
    expect(ago('2026-10-31T09:00:00Z', now)).toBe('3 ч назад');
    expect(ago('2026-10-22T12:00:00Z', now)).toBe('9 дней назад');
  });
  it('tells a journal line with was and became', () => {
    const line = describe({
      id: 'a',
      at: '2026-10-31T10:00:00Z',
      actor: { userId: 'u', name: 'Андрей' },
      action: 'limit.set',
      target: { kind: 'user', id: 'p', label: 'Олег' },
      before: { limitRub: 20_000 },
      after: { limitRub: 30_000 },
      self: false,
    });
    expect(line).toEqual({ who: 'Андрей', what: 'лимит Олег: 20 тыс ₽ → 30 тыс ₽' });
  });
});
