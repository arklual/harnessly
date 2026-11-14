// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { LIBRARY_SAFETY_MS, watchLibrary } from '@/next/features/designly/library-watch';

const flush = async () => {
  for (let i = 0; i < 10; i += 1) await Promise.resolve();
};

describe('watchLibrary (HRN-1418)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('reads the library when the hub says it changed, not on the first answer', async () => {
    const answers: ((rev: number) => void)[] = [];
    const wait = vi.fn(
      (_m: string, _since: number) => new Promise<number>((resolve) => answers.push(resolve)),
    );
    const read = vi.fn(() => Promise.resolve());
    const stop = watchLibrary('m1', read, 5000, wait);
    await flush();
    answers[0]?.(4);
    await flush();
    expect(read).not.toHaveBeenCalled();
    expect(wait).toHaveBeenLastCalledWith('m1', 4, expect.anything());
    answers[1]?.(4);
    await flush();
    expect(read).not.toHaveBeenCalled();
    answers[2]?.(6);
    await flush();
    expect(read).toHaveBeenCalledTimes(1);
    expect(wait).toHaveBeenLastCalledWith('m1', 6, expect.anything());
    // The fast timer of the fallback is not running: only the slow one behind the poll.
    await vi.advanceTimersByTimeAsync(5000);
    expect(read).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(LIBRARY_SAFETY_MS);
    expect(read).toHaveBeenCalledTimes(2);
    stop();
  });

  it('asks on the timer, as before, where the hub has no long poll', async () => {
    const wait = vi.fn(() => Promise.reject(new Error('404 home_off')));
    const read = vi.fn(() => Promise.resolve());
    const stop = watchLibrary('m1', read, 5000, wait);
    await flush();
    await vi.advanceTimersByTimeAsync(5000);
    expect(read).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(5000);
    expect(read).toHaveBeenCalledTimes(2);
    stop();
  });
});
