// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { useViewMemory } from '@/next/features/designly/use-view-memory';
import { VIEW_DEFAULT, writeView, type IViewMemory } from '@/next/features/designly/view-memory';

afterEach(() => localStorage.clear());

const seed: IViewMemory = { ...VIEW_DEFAULT, device: 'ip15', chrome: false, page: 'a.html' };

describe('the view a design opens with', () => {
  it('is the seed where the browser kept nothing', () => {
    const { result } = renderHook(() => useViewMemory('guest:t', seed));
    expect(result.current.initial).toBe(seed);
  });

  it('is what the browser kept, over the seed', () => {
    writeView('guest:t', { ...seed, device: 'fill' });
    const { result } = renderHook(() => useViewMemory('guest:t', seed));
    expect(result.current.initial.device).toBe('fill');
  });

  it('is the default without a seed', () => {
    const { result } = renderHook(() => useViewMemory('d'));
    expect(result.current.initial).toBe(VIEW_DEFAULT);
  });
});
