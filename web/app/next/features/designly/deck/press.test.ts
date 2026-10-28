import { describe, expect, it, vi } from 'vitest';
import { createPress } from '@/next/features/designly/deck/press';

const setup = () => {
  vi.useFakeTimers();
  const log: string[] = [];
  const press = createPress(
    {
      onRing: (p) => log.push(p ? 'ring' : 'unring'),
      onFire: () => log.push('fire'),
      onTap: (text) => log.push(`tap:${text}`),
    },
    { setTimeout: (f, ms) => setTimeout(f, ms), clearTimeout: (id) => clearTimeout(id as never) },
  );
  return { press, log };
};

describe('createPress', () => {
  it('rings at 250 ms and fires at 730 ms', () => {
    const { press, log } = setup();
    press.down(0, 0, { px: 1, py: 2 }, false);
    vi.advanceTimersByTime(249);
    expect(log).toEqual([]);
    vi.advanceTimersByTime(1);
    expect(log).toEqual(['ring']);
    vi.advanceTimersByTime(480);
    expect(log).toEqual(['ring', 'unring', 'fire']);
  });
  it('moving more than 10 px cancels', () => {
    const { press, log } = setup();
    press.down(0, 0, { px: 1, py: 2 }, false);
    vi.advanceTimersByTime(300);
    press.move(11, 0);
    vi.advanceTimersByTime(1000);
    expect(log).toEqual(['ring', 'unring']);
  });
  it('a small move does not', () => {
    const { press, log } = setup();
    press.down(0, 0, { px: 1, py: 2 }, false);
    press.move(5, 5);
    vi.advanceTimersByTime(800);
    expect(log).toEqual(['ring', 'unring', 'fire']);
  });
  it('a lift before the ring is a tap', () => {
    const { press, log } = setup();
    press.down(0, 0, { px: 1, py: 2 }, true);
    vi.advanceTimersByTime(100);
    press.up();
    vi.advanceTimersByTime(1000);
    expect(log).toEqual(['tap:true']);
  });
});
