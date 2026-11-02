import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IEditProgress, IImageEdit, IImageEditHost } from '@/next/features/image-edit/host';
import {
  RUN_MAX_MISSES,
  RUN_POLL_MS,
  RUN_STATE,
  dismissRun,
  resetRuns,
  retryRun,
  runOf,
  startRun,
} from '@/next/features/image-edit/run-store';

const from = { id: '/a.png', src: '/a' };
const edit: IImageEdit = { from, mode: 'text', prompt: 'night' };
const result = { id: '/b.png', src: '/b' };

const host = (over: Partial<IImageEditHost>): IImageEditHost => ({
  start: vi.fn().mockResolvedValue('job-1'),
  read: vi.fn().mockResolvedValue({ state: 'running', versions: [] } satisfies IEditProgress),
  versions: vi.fn().mockResolvedValue([]),
  ...over,
});

describe('run store', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    resetRuns();
    vi.useRealTimers();
  });

  it('runs on after the viewer is gone, and ends done with the new versions', async () => {
    const read = vi
      .fn<IImageEditHost['read']>()
      .mockResolvedValueOnce({ state: 'running', versions: [] })
      .mockResolvedValueOnce({ state: 'done', versions: [result] });
    startRun('/a.png', host({ read }), edit);
    expect(runOf('/a.png')?.state).toBe(RUN_STATE.running);
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS * 3);
    expect(runOf('/a.png')).toMatchObject({ state: RUN_STATE.done, versions: [result] });
    dismissRun('/a.png');
    expect(runOf('/a.png')).toBeNull();
  });

  it('chooses the new version itself, with no viewer around', async () => {
    const chosen = vi.fn();
    const read = vi.fn().mockResolvedValue({ state: 'done', versions: [result] } satisfies IEditProgress);
    startRun(from.id, host({ read, chosen }), edit);
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS + 1);
    expect(chosen).toHaveBeenCalledTimes(1);
    expect(chosen).toHaveBeenCalledWith(result);
  });

  it('shows the reason of a refusal and starts the same change again on retry', async () => {
    const start = vi
      .fn<IImageEditHost['start']>()
      .mockRejectedValueOnce(new Error('too many pictures at once'))
      .mockResolvedValueOnce('job-2');
    startRun('/a.png', host({ start }), edit);
    await vi.advanceTimersByTimeAsync(0);
    expect(runOf('/a.png')).toMatchObject({ state: RUN_STATE.failed, error: 'too many pictures at once' });
    retryRun('/a.png');
    expect(runOf('/a.png')?.state).toBe(RUN_STATE.running);
    expect(start).toHaveBeenLastCalledWith(edit);
  });

  it('a failed job keeps the server reason', async () => {
    const read = vi
      .fn<IImageEditHost['read']>()
      .mockResolvedValue({ state: 'failed', versions: [], error: 'refused' });
    startRun('/a.png', host({ read }), edit);
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS + 1);
    expect(runOf('/a.png')).toMatchObject({ state: RUN_STATE.failed, error: 'refused' });
  });

  it('a few failed reads are borne, a run of them ends it', async () => {
    const read = vi.fn<IImageEditHost['read']>().mockRejectedValue(new Error('offline'));
    startRun('/a.png', host({ read }), edit);
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS * (RUN_MAX_MISSES - 1) + 1);
    expect(runOf('/a.png')?.state).toBe(RUN_STATE.running);
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS * 2);
    expect(runOf('/a.png')).toMatchObject({ state: RUN_STATE.failed, error: 'offline' });
  });

  it('a dismissed run stops asking', async () => {
    const read = vi.fn<IImageEditHost['read']>().mockResolvedValue({ state: 'running', versions: [] });
    startRun('/a.png', host({ read }), edit);
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS + 1);
    const calls = read.mock.calls.length;
    dismissRun('/a.png');
    await vi.advanceTimersByTimeAsync(RUN_POLL_MS * 5);
    expect(read.mock.calls.length).toBe(calls);
  });
});
