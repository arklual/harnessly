// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MachineId } from '@/next/protocol/v2';

const calls = vi.hoisted(() => ({ n: 0 }));
vi.mock('@/next/core/net/runtime-json', () => ({
  runtimeJson: vi.fn(async () => {
    calls.n += 1;
    return { notices: [], muted: [] };
  }),
  postJson: (b: unknown) => b,
  RuntimeError: class extends Error {},
}));

import { SLOT_POLL_MS, useSlotNotices } from './slot-api';
import { setSlotStreamFactory, type ISlotSource } from './slot-stream';

const m = 'm1' as MachineId;

describe('the slot stream (HRN-1421)', () => {
  let source: ISlotSource & { fire: (t: string) => void };
  let restore: ReturnType<typeof setSlotStreamFactory>;
  beforeEach(() => {
    vi.useFakeTimers();
    calls.n = 0;
    const handlers: Record<string, () => void> = {};
    source = {
      onopen: null,
      onerror: null,
      addEventListener: (t, fn) => {
        handlers[t] = fn;
      },
      close: () => undefined,
      fire: (t) => handlers[t]?.(),
    };
    restore = setSlotStreamFactory(() => source);
  });
  afterEach(() => {
    setSlotStreamFactory(restore);
    vi.useRealTimers();
  });

  it('does not poll every 1.5 s while the stream is open, and looks when it says the slot moved', async () => {
    renderHook(() => useSlotNotices(m));
    await act(async () => {
      source.onopen?.();
      await vi.advanceTimersByTimeAsync(0);
    });
    const after = calls.n;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(SLOT_POLL_MS * 8);
    });
    expect(calls.n).toBe(after);
    await act(async () => {
      source.fire('slot');
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(calls.n).toBe(after + 1);
  });

  it('polls as before while there is no stream', async () => {
    renderHook(() => useSlotNotices(m));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(SLOT_POLL_MS * 3);
    });
    expect(calls.n).toBeGreaterThanOrEqual(3);
  });
});
