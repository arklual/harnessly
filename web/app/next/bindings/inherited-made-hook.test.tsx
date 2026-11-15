// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ConvId, MachineId, MadeItem } from '@/next/protocol/v2';

const made = vi.hoisted(() => ({ items: [] as MadeItem[] }));
vi.mock('@/next/commands/pages', () => ({
  loadMade: () => Promise.resolve({ ok: true, value: { items: made.items } }),
}));
vi.mock('@/next/stores/machines', () => ({ hasFeature: () => true }));

import { INHERITED_DEBOUNCE_MS, useInheritedMade } from '@/next/bindings/inherited-made';

const machine = 'm1' as MachineId;
const conv = 'tab-a' as ConvId;
const mr: MadeItem = {
  kind: 'mr',
  ref: 'https://git.example.com/a/b/-/merge_requests/1',
  title: 'Fix',
  op: 'created',
  at: 1,
  cover: null,
  designKind: null,
  from: { kind: 'subchat', id: 'tab-kid', title: 'Kid' },
};

const settle = async () => {
  await act(async () => {
    vi.advanceTimersByTime(INHERITED_DEBOUNCE_MS);
    await Promise.resolve();
  });
};

describe('what the subchats made keeps its list while the answer is the same (HRN-1491)', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  it('an empty answer, asked again, does not hand the feed a new list', async () => {
    made.items = [];
    const { result, rerender } = renderHook(({ moved }) => useInheritedMade(machine, conv, true, moved), {
      initialProps: { moved: 'a' },
    });
    const first = result.current;
    await settle();
    expect(result.current).toBe(first);
    rerender({ moved: 'b' });
    await settle();
    expect(result.current).toBe(first);
  });

  it('the same things again keep the list; a new thing gives a new one', async () => {
    made.items = [mr];
    const { result, rerender } = renderHook(({ moved }) => useInheritedMade(machine, conv, true, moved), {
      initialProps: { moved: 'a' },
    });
    await settle();
    const first = result.current;
    expect(first).toHaveLength(1);
    made.items = [{ ...mr }];
    rerender({ moved: 'b' });
    await settle();
    expect(result.current).toBe(first);
    made.items = [mr, { ...mr, ref: 'https://git.example.com/a/b/-/merge_requests/2' }];
    rerender({ moved: 'c' });
    await settle();
    expect(result.current).toHaveLength(2);
  });
});
