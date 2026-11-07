import { describe, expect, it } from 'vitest';
import { formatProgress, formatRemaining, percentOf } from '@/next/ui/transfer';

describe('transfer', () => {
  it('is 100 only when every byte is there', () => {
    expect(percentOf({ sent: 999, size: 1000, speed: 0 })).toBe(99);
    expect(percentOf({ sent: 1000, size: 1000, speed: 0 })).toBe(100);
    expect(percentOf({ sent: 0, size: 0, speed: 0 })).toBe(0);
  });

  it('leaves out the speed and the time left until the speed is known', () => {
    const line = formatProgress({ sent: 50, size: 100, speed: 0 });
    expect(line.split(' · ')).toHaveLength(2);
    expect(formatProgress({ sent: 50 << 20, size: 100 << 20, speed: 10 << 20 }).split(' · ')).toHaveLength(4);
  });

  it('rounds the time left up', () => {
    expect(formatRemaining(0.2)).not.toBe('');
    expect(formatRemaining(61)).toContain('2');
    expect(formatRemaining(Number.NaN)).toBe('');
  });
});
