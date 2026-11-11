// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { startQuietPoll } from '@/next/bindings/quiet-poll';

const MS = 5000;

const setHidden = (hidden: boolean) => {
  Object.defineProperty(document, 'hidden', { configurable: true, get: () => hidden });
  document.dispatchEvent(new Event('visibilitychange'));
};

/** An ask that ends when the test says so. */
const slowAsk = () => {
  const signals: AbortSignal[] = [];
  const ends: (() => void)[] = [];
  const ask = vi.fn((signal: AbortSignal) => {
    signals.push(signal);
    return new Promise<void>((resolve) => ends.push(resolve));
  });
  return { ask, signals, end: (i: number) => ends[i]?.() };
};

describe('startQuietPoll', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setHidden(false);
  });
  afterEach(() => {
    vi.useRealTimers();
    setHidden(false);
  });

  it('asks one at a time: the next ask waits for the previous one to end', async () => {
    const { ask, end } = slowAsk();
    const stop = startQuietPoll(ask, MS);
    expect(ask).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(MS);
    expect(ask).toHaveBeenCalledTimes(1);
    // The answer takes three periods: nothing else is asked meanwhile.
    await vi.advanceTimersByTimeAsync(3 * MS);
    expect(ask).toHaveBeenCalledTimes(1);
    end(0);
    await vi.advanceTimersByTimeAsync(MS - 1);
    expect(ask).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(ask).toHaveBeenCalledTimes(2);
    stop();
  });

  it('goes on after an ask that failed', async () => {
    const ask = vi.fn(() => Promise.reject(new Error('no')));
    const stop = startQuietPoll(ask, MS);
    await vi.advanceTimersByTimeAsync(MS);
    await vi.advanceTimersByTimeAsync(MS);
    expect(ask).toHaveBeenCalledTimes(2);
    stop();
  });

  it('asks nothing while the page is hidden and asks at once when it is back', async () => {
    const ask = vi.fn(() => Promise.resolve());
    const stop = startQuietPoll(ask, MS);
    setHidden(true);
    await vi.advanceTimersByTimeAsync(10 * MS);
    expect(ask).not.toHaveBeenCalled();
    setHidden(false);
    expect(ask).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(MS);
    expect(ask).toHaveBeenCalledTimes(2);
    stop();
  });

  it('does not ask twice when the page comes back during an ask', async () => {
    const { ask, end } = slowAsk();
    const stop = startQuietPoll(ask, MS);
    await vi.advanceTimersByTimeAsync(MS);
    setHidden(true);
    setHidden(false);
    expect(ask).toHaveBeenCalledTimes(1);
    end(0);
    await vi.advanceTimersByTimeAsync(MS);
    expect(ask).toHaveBeenCalledTimes(2);
    stop();
  });

  it('stopping aborts the ask in flight and asks no more', async () => {
    const { ask, signals, end } = slowAsk();
    const stop = startQuietPoll(ask, MS);
    await vi.advanceTimersByTimeAsync(MS);
    expect(signals[0]?.aborted).toBe(false);
    stop();
    expect(signals[0]?.aborted).toBe(true);
    end(0);
    await vi.advanceTimersByTimeAsync(10 * MS);
    setHidden(false);
    expect(ask).toHaveBeenCalledTimes(1);
  });
});
