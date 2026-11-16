// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const runtimeJson = vi.fn();
vi.mock('@/next/core/net/runtime-json', async (real) => ({
  ...(await real<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (...a: unknown[]) => runtimeJson(...a),
}));

const { installedRetryMs, useInstalledApps } = await import('./api');

describe('the installed apps of a machine', () => {
  afterEach(() => {
    vi.useRealTimers();
    runtimeJson.mockReset();
  });

  it('are read again by themselves after a read that failed, so the start page does not stay empty', async () => {
    vi.useFakeTimers();
    runtimeJson
      .mockRejectedValueOnce(new Error('timeout'))
      .mockResolvedValueOnce({ apps: [{ id: 'acme.day', views: [] }], viewPort: 1, slots: true });
    const { result } = renderHook(() => useInstalledApps('m-retry' as never));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current).toMatchObject({ list: null, error: 'timeout' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(installedRetryMs(0));
    });
    expect(runtimeJson).toHaveBeenCalledTimes(2);
    expect(result.current.list?.apps.map((a) => a.id)).toEqual(['acme.day']);
  });

  it('wait longer after each failure, at most a minute', () => {
    expect(installedRetryMs(0)).toBe(3_000);
    expect(installedRetryMs(1)).toBe(6_000);
    expect(installedRetryMs(10)).toBe(60_000);
  });
});
