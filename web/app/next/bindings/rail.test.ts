// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const KEY = 'hx-sidebar-collapsed';
type Rail = typeof import('@/next/bindings/rail');

// The rail's state lives in its module: each case starts from a fresh one over the stored choice.
const load = async (kept: '0' | '1'): Promise<Rail & { shown: () => boolean }> => {
  localStorage.setItem(KEY, kept);
  vi.resetModules();
  const rail = await import('@/next/bindings/rail');
  const hook = renderHook(() => rail.useRail());
  return { ...rail, shown: () => hook.result.current };
};

describe('the sidebar and the right pane on a narrow window (HRN-674)', () => {
  beforeEach(() => localStorage.clear());
  afterEach(cleanup);

  it('folds for the pane and opens again when it shuts, keeping the stored choice', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(1280));
    expect(rail.shown()).toBe(true);
    expect(localStorage.getItem(KEY)).toBe('0');
    act(() => rail.restoreRailAfterPane());
    expect(rail.shown()).toBe(false);
  });

  it('leaves a wide window as it is', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(rail.PANE_FOLD_MAX_W));
    expect(rail.shown()).toBe(false);
  });

  it('does not open a sidebar the person had folded', async () => {
    const rail = await load('1');
    act(() => rail.foldRailForPane(1280));
    act(() => rail.restoreRailAfterPane());
    expect(rail.shown()).toBe(true);
  });

  it('keeps what the person did while the pane was open', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(1280));
    act(() => rail.toggleRail());
    expect(rail.shown()).toBe(false);
    act(() => rail.toggleRail());
    act(() => rail.restoreRailAfterPane());
    expect(rail.shown()).toBe(true);
    expect(localStorage.getItem(KEY)).toBe('1');
  });
});
