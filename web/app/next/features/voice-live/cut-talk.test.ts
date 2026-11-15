// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';

describe('a talk cut by a reload', () => {
  afterEach(() => {
    window.sessionStorage.clear();
    vi.resetModules();
  });

  it('is told once in the chat it was in, after the page comes again', async () => {
    const first = await import('./cut-talk');
    first.keepTalk('c1');
    vi.resetModules();
    const after = await import('./cut-talk');
    const { renderHook } = await import('@testing-library/react');
    const shown = renderHook(() => after.useCutTalk('c1'));
    const other = renderHook(() => after.useCutTalk('c2'));
    expect(shown.result.current).toBe(true);
    expect(other.result.current).toBe(false);
    expect(window.sessionStorage.getItem('hx-voice-talk')).toBeNull();
  });

  it('says nothing after a talk ended as asked', async () => {
    const first = await import('./cut-talk');
    first.keepTalk('c1');
    first.keepTalk(null);
    vi.resetModules();
    const after = await import('./cut-talk');
    const { renderHook } = await import('@testing-library/react');
    expect(renderHook(() => after.useCutTalk('c1')).result.current).toBe(false);
  });
});
