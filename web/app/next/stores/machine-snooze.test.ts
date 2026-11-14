import { describe, expect, it } from 'vitest';
import { SNOOZE_MS, isSnoozed, parseSnoozes, snoozeLevel } from '@/next/stores/machine-snooze';

describe('machine snooze (HRN-1377)', () => {
  it('holds for its machine and level until it ends', () => {
    const now = 1_000_000;
    const snoozes = { m1: { level: snoozeLevel('disk', false), until: now + SNOOZE_MS } };
    expect(isSnoozed(snoozes, 'm1', 'disk:warn', now)).toBe(true);
    expect(isSnoozed(snoozes, 'm1', 'disk:stop', now)).toBe(false);
    expect(isSnoozed(snoozes, 'm2', 'disk:warn', now)).toBe(false);
    expect(isSnoozed(snoozes, 'm1', 'disk:warn', now + SNOOZE_MS)).toBe(false);
  });

  it('reads back only what still runs and is well formed', () => {
    const raw = JSON.stringify({
      a: { level: 'io:stop', until: 50 },
      b: { level: 'io:stop', until: 5 },
      c: 1,
    });
    expect(parseSnoozes(raw, 10)).toEqual({ a: { level: 'io:stop', until: 50 } });
    expect(parseSnoozes('{', 10)).toEqual({});
    expect(parseSnoozes(null, 10)).toEqual({});
  });
});
