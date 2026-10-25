// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import usePending, { PENDING, PENDING_SLOW_MS, PENDING_TIMEOUT_MS } from '@/next/ui/use-pending';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const deferred = () => {
  let resolve!: () => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<void>((ok, fail) => {
    resolve = ok;
    reject = fail;
  });
  return { promise, resolve, reject };
};

describe('usePending', () => {
  it('turns busy at once, slow after 3 s, idle on the answer', async () => {
    const { result } = renderHook(() => usePending<'send'>());
    const answer = deferred();
    act(() => result.current.run('send', () => answer.promise));
    expect(result.current.phase).toBe(PENDING.busy);
    expect(result.current.key).toBe('send');
    act(() => void vi.advanceTimersByTime(PENDING_SLOW_MS));
    expect(result.current.phase).toBe(PENDING.slow);
    await act(async () => answer.resolve());
    expect(result.current.phase).toBe(PENDING.idle);
    act(() => void vi.advanceTimersByTime(PENDING_TIMEOUT_MS));
    expect(result.current.phase).toBe(PENDING.idle);
  });

  it('gives up after 15 s without an answer, and a retry runs the action again', async () => {
    const { result } = renderHook(() => usePending<'send'>());
    const action = vi.fn(() => new Promise<void>(() => {}));
    act(() => result.current.run('send', action));
    act(() => void vi.advanceTimersByTime(PENDING_TIMEOUT_MS));
    expect(result.current.phase).toBe(PENDING.failed);
    act(() => result.current.retry());
    expect(result.current.phase).toBe(PENDING.busy);
    expect(action).toHaveBeenCalledTimes(2);
  });

  it('fails on a rejection and ignores a second press while it runs', async () => {
    const { result } = renderHook(() => usePending<'a' | 'b'>());
    const answer = deferred();
    const second = vi.fn(() => Promise.resolve());
    act(() => result.current.run('a', () => answer.promise));
    act(() => result.current.run('b', second));
    expect(second).not.toHaveBeenCalled();
    await act(async () => answer.reject(new Error('no')));
    expect(result.current.phase).toBe(PENDING.failed);
    expect(result.current.key).toBe('a');
    act(() => result.current.reset());
    expect(result.current.phase).toBe(PENDING.idle);
  });
});
