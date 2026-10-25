// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useDeadline } from '@/next/ui/use-deadline';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('useDeadline', () => {
  it('expires only after the wait has held for the whole time, and not while there is nothing to wait for', () => {
    const { result, rerender } = renderHook(({ on }) => useDeadline(on, 1000), {
      initialProps: { on: true },
    });
    act(() => void vi.advanceTimersByTime(999));
    expect(result.current.expired).toBe(false);
    act(() => void vi.advanceTimersByTime(2));
    expect(result.current.expired).toBe(true);
    rerender({ on: false });
    expect(result.current.expired).toBe(false);
  });

  it('restart waits the whole time again', () => {
    const { result } = renderHook(() => useDeadline(true, 1000));
    act(() => void vi.advanceTimersByTime(1001));
    expect(result.current.expired).toBe(true);
    act(() => result.current.restart());
    expect(result.current.expired).toBe(false);
    act(() => void vi.advanceTimersByTime(1001));
    expect(result.current.expired).toBe(true);
  });
});
