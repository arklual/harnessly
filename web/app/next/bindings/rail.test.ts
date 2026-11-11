// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const KEY = 'hx-sidebar-collapsed';
const FULL_KEY = 'hx-sidebar-collapsed-full';
type Rail = typeof import('@/next/bindings/rail');

// The rail's state lives in its module: each case starts from a fresh one over the stored choice.
const load = async (kept: '0' | '1' | null, full: '0' | '1' | null = null) => {
  if (kept !== null) localStorage.setItem(KEY, kept);
  if (full !== null) localStorage.setItem(FULL_KEY, full);
  vi.resetModules();
  const rail: Rail = await import('@/next/bindings/rail');
  const hook = renderHook(() => rail.useRail());
  return { ...rail, shown: () => hook.result.current };
};

describe('the sidebar and the right pane on a narrow window (HRN-674)', () => {
  beforeEach(() => localStorage.clear());
  afterEach(cleanup);

  it('folds for the pane, keeping the stored choice', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(1280));
    expect(rail.shown()).toBe(true);
    expect(localStorage.getItem(KEY)).toBe('0');
  });

  it('leaves a wide window as it is', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(rail.PANE_FOLD_MAX_W));
    expect(rail.shown()).toBe(false);
  });

  it('opens only by the person, and keeps what the person chose', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(1280));
    act(() => rail.toggleRail());
    expect(rail.shown()).toBe(false);
    expect(localStorage.getItem(KEY)).toBe('0');
    act(() => rail.toggleRail());
    expect(rail.shown()).toBe(true);
    expect(localStorage.getItem(KEY)).toBe('1');
  });
});

describe('moving between tabs and screens never opens the sidebar (HRN-1160)', () => {
  beforeEach(() => localStorage.clear());
  afterEach(cleanup);

  it('stays folded when the pane goes with its chat: another tab, a design, an app', async () => {
    const rail = await load('0');
    act(() => rail.foldRailForPane(1280));
    // The chat with the pane is left for a design, an app, and back to a chat without the pane.
    act(() => rail.setRailMode(rail.RAIL_MODE.full));
    expect(rail.shown()).toBe(true);
    act(() => rail.setRailMode(rail.RAIL_MODE.screen));
    expect(rail.shown()).toBe(true);
  });

  it('starts folded on an app or a design, and keeps the person opening it there', async () => {
    const rail = await load(null);
    expect(rail.shown()).toBe(false);
    act(() => rail.setRailMode(rail.RAIL_MODE.full));
    expect(rail.shown()).toBe(true);
    act(() => rail.toggleRail());
    expect(rail.shown()).toBe(false);
    expect(localStorage.getItem(FULL_KEY)).toBe('0');
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it('keeps each choice apart: folded on the chats stays folded after an app', async () => {
    const rail = await load('1', '0');
    expect(rail.shown()).toBe(true);
    act(() => rail.setRailMode(rail.RAIL_MODE.full));
    expect(rail.shown()).toBe(false);
    act(() => rail.setRailMode(rail.RAIL_MODE.screen));
    expect(rail.shown()).toBe(true);
  });
});
