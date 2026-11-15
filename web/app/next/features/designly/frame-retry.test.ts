// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const lines: { ev: string; src: string }[] = [];
vi.mock('@/next/core/journal', () => ({
  journal: (line: { ev: string; src: string }) => lines.push(line),
}));

import {
  FRAME_SILENT_MS,
  FRAME_SILENT_TRIES,
  frameFailure,
  frameRetryDelay,
  useFrameRetry,
  type IFrameRetryOptions,
} from '@/next/features/designly/frame-retry';

describe('frame retry', () => {
  it('waits longer after each refusal, up to half a minute', () => {
    expect(frameRetryDelay(0)).toBe(2000);
    expect(frameRetryDelay(1)).toBe(4000);
    expect(frameRetryDelay(3)).toBe(16000);
    expect(frameRetryDelay(10)).toBe(30000);
  });

  it('reads a refusal and the hub’s reload page of the frame, nothing else', () => {
    expect(frameFailure({ hlViewError: { kind: 'refused', status: 502 } })).toBe('refused');
    expect(frameFailure({ type: 'hl-preview-reload', path: '/a/harnessly/p/x.dc.html' })).toBe('reload');
    expect(frameFailure({ hlViewError: { kind: 'script' } })).toBeNull();
    expect(frameFailure({ type: 'tk:pick' })).toBeNull();
    expect(frameFailure(null)).toBeNull();
    expect(frameFailure('refused')).toBeNull();
  });
});

// The board's frame of Sinopia: an agent shows another board (show_board), then reloads it (reload_board); the frame
// gets the hub's page instead of the board («hub refused to connect» at 11:03).
describe('the board frame after show_board → reload_board', () => {
  const SLOT = 'https://hub.example/m/abc/__pv/1-0123456789abcdef0123456789abcdef';
  const frameWindow = {} as Window;
  const frameRef = { current: { contentWindow: frameWindow } as HTMLIFrameElement };
  const fromFrame = (data: unknown, source: Window = frameWindow) =>
    act(() => {
      window.dispatchEvent(new MessageEvent('message', { data, source, origin: 'null' }));
    });

  beforeEach(() => {
    vi.useFakeTimers();
    lines.length = 0;
  });
  afterEach(() => vi.useRealTimers());

  const mount = (onRetry: () => void) =>
    renderHook<ReturnType<typeof useFrameRetry>, IFrameRetryOptions>((p) => useFrameRetry(p), {
      initialProps: {
        frameRef,
        src: `${SLOT}/a/harnessly/p/a.dc.html`,
        frameKey: `${SLOT}/a/harnessly/p/a.dc.html#0`,
        loadedFor: `${SLOT}/a/harnessly/p/a.dc.html#0`,
        libReadyFor: `${SLOT}/a/harnessly/p/a.dc.html#0`,
        onRetry,
      },
    });

  it('the hub’s reload page for the board past its slot opens the frame anew and says so in the journal', () => {
    const onRetry = vi.fn();
    const { rerender } = mount(onRetry);
    // show_board: another board in the same slot; reload_board: the same address loads again.
    const b = `${SLOT}/a/harnessly/p/b.dc.html`;
    rerender({ frameRef, src: b, frameKey: `${b}#1`, loadedFor: null, libReadyFor: null, onRetry });
    fromFrame({ type: 'hl-preview-reload', path: '/a/harnessly/p/b.dc.html' });
    // The load of the hub's page comes after its message: the wait set by the message stands.
    rerender({ frameRef, src: b, frameKey: `${b}#1`, loadedFor: `${b}#1`, libReadyFor: null, onRetry });
    act(() => vi.advanceTimersByTime(frameRetryDelay(0)));
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(lines).toEqual([expect.objectContaining({ src: 'sinopia', ev: 'board.frame-reload' })]);
  });

  it('a refusal the page can not read (loaded, the board never says it is there) opens it anew, a few times at most', () => {
    const onRetry = vi.fn();
    const { rerender } = mount(onRetry);
    for (let i = 1; i <= FRAME_SILENT_TRIES + 2; i += 1) {
      const key = `${SLOT}/a/harnessly/p/b.dc.html#${i}`;
      rerender({ frameRef, src: key, frameKey: key, loadedFor: key, libReadyFor: null, onRetry });
      act(() => vi.advanceTimersByTime(FRAME_SILENT_MS + 30000));
    }
    expect(onRetry).toHaveBeenCalledTimes(FRAME_SILENT_TRIES);
    expect(lines.every((l) => l.ev === 'board.frame-silent')).toBe(true);
    expect(lines).toHaveLength(FRAME_SILENT_TRIES);
  });

  it('a board that draws resets the count, and a refusal of another frame is not this one’s', () => {
    const onRetry = vi.fn();
    mount(onRetry);
    fromFrame({ hlViewError: { kind: 'refused', status: 502 } }, {} as Window);
    act(() => vi.advanceTimersByTime(FRAME_SILENT_MS + 30000));
    expect(onRetry).not.toHaveBeenCalled();
    expect(lines).toHaveLength(0);
  });
});
