import { afterEach, describe, expect, it, vi } from 'vitest';
import type { IInstalledApp } from '@/next/features/app-views/api';

const load = vi.fn();
const refresh = vi.fn();
vi.mock('@/next/features/app-views/api', () => ({
  loadInstalledApps: (...a: unknown[]) => load(...a),
  refreshInstalledApps: (...a: unknown[]) => refresh(...a),
}));

const { appStamp, resetAppUpdates, updated, watchAppUpdate } =
  await import('@/next/features/app-views/app-updates');

const app = (version: string, updatedAt?: number) =>
  ({ id: 'harnessly.boards', version, updatedAt }) as IInstalledApp;

describe('app updates (HRN-1478)', () => {
  afterEach(() => {
    vi.useRealTimers();
    load.mockReset();
    refresh.mockReset();
    resetAppUpdates();
  });

  it('a new version or the same one put there again is an update; a gone app is not', () => {
    expect(appStamp(app('0.5.0', 7))).toBe('0.5.0@7');
    expect(updated(app('0.4.0', 1), [app('0.5.0', 2)])).toBe(true);
    expect(updated(app('0.5.0', 1), [app('0.5.0', 2)])).toBe(true);
    expect(updated(app('0.5.0', 1), [app('0.5.0', 1)])).toBe(false);
    expect(updated(app('0.5.0', 1), [])).toBe(false);
  });

  it('looks while the view is up and reads the list again on a new version', async () => {
    vi.useFakeTimers();
    load.mockResolvedValue({ apps: [app('0.5.0', 2)] });
    const stop = watchAppUpdate('m1' as never, app('0.4.0', 1), 1000);
    await vi.advanceTimersByTimeAsync(1000);
    expect(load).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledWith('m1');
    stop();
    await vi.advanceTimersByTimeAsync(5000);
    expect(load).toHaveBeenCalledTimes(1);
  });
});
