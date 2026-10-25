// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PRESENCE, useLeavingList, usePresence } from '@/next/ui/presence';

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('usePresence', () => {
  it('keeps the last value through its exit, then lets it go', () => {
    const { result, rerender } = renderHook(({ value }) => usePresence(value, 200), {
      initialProps: { value: 'a' as string | null },
    });
    expect(result.current).toEqual({ value: 'a', state: PRESENCE.open });
    rerender({ value: null });
    expect(result.current).toEqual({ value: 'a', state: PRESENCE.closing });
    act(() => void vi.advanceTimersByTime(200));
    expect(result.current.value).toBeNull();
  });

  it('comes back open when a value returns during the exit', () => {
    const { result, rerender } = renderHook(({ value }) => usePresence(value, 200), {
      initialProps: { value: 'a' as string | null },
    });
    rerender({ value: null });
    rerender({ value: 'b' });
    expect(result.current).toEqual({ value: 'b', state: PRESENCE.open });
    act(() => void vi.advanceTimersByTime(200));
    expect(result.current.value).toBe('b');
  });
});

describe('useLeavingList', () => {
  it('keeps a removed item in its place while it leaves', () => {
    const a = { id: 'a' };
    const b = { id: 'b' };
    const c = { id: 'c' };
    const { result, rerender } = renderHook(({ items }) => useLeavingList(items, (item) => item.id, 220), {
      initialProps: { items: [a, b, c] },
    });
    rerender({ items: [a, c] });
    expect(result.current.map(({ item, leaving }) => `${item.id}${leaving ? '-' : ''}`)).toEqual([
      'a',
      'b-',
      'c',
    ]);
    act(() => void vi.advanceTimersByTime(220));
    expect(result.current.map(({ item }) => item.id)).toEqual(['a', 'c']);
  });
});
