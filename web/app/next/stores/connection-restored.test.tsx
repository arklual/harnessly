// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { setScheduler } from '@/next/core/store';
import { connectionStore, useLinkRestored } from '@/next/stores/connection';
import type { MachineId, Ms } from '@/next/protocol/v2';

const M = 'm-restored' as MachineId;
const at = 1 as Ms;
beforeEach(() => setScheduler('sync'));
afterEach(() => {
  cleanup();
  setScheduler('frame');
});

describe('useLinkRestored', () => {
  it('counts only the returns of a link that was down, not the first connect', () => {
    const { result } = renderHook(() => useLinkRestored(M));
    act(() => connectionStore.set(M, { k: 'live', since: at }));
    expect(result.current).toBe(0);
    act(() => connectionStore.set(M, { k: 'stale', since: at }));
    expect(result.current).toBe(0);
    act(() => connectionStore.set(M, { k: 'live', since: at }));
    expect(result.current).toBe(1);
    act(() => connectionStore.set(M, { k: 'offline', retryAt: at, cause: 'network' }));
    act(() => connectionStore.set(M, { k: 'connecting', attempt: 1 }));
    expect(result.current).toBe(1);
    act(() => connectionStore.set(M, { k: 'live', since: at }));
    expect(result.current).toBe(2);
  });
});
