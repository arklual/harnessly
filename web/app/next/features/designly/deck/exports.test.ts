// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IDeckExportJob } from '@/next/features/designly/deck/types';

const api = vi.hoisted(() => ({
  startDeckExport: vi.fn(),
  fetchDeckExport: vi.fn(),
  deckExportHref: vi.fn((_m: string, artifact: string, job: string) => `/dl?artifact=${artifact}&job=${job}`),
}));
const toast = vi.hoisted(() => vi.fn());
vi.mock('@/next/features/designly/deck/api', () => api);
vi.mock('@/next/ui/toast', () => ({ toast }));
vi.mock('@/next/i18n', () => ({ t: (key: string) => key }));

import { DECK_EXPORT_POLL_MS, followDeckExport, runDeckExport } from '@/next/features/designly/deck/exports';

const job = (over: Partial<IDeckExportJob>): IDeckExportJob => ({
  id: 'j1',
  format: 'pdf',
  state: 'queued',
  progress: 0,
  file: null,
  error: null,
  at: '2026-10-27T10:00:00Z',
  ...over,
});

describe('runDeckExport', () => {
  let clicked: string[];
  beforeEach(() => {
    vi.useFakeTimers();
    clicked = [];
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      clicked.push(this.getAttribute('href') ?? '');
    });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it('says it began, follows the job and downloads the file', async () => {
    api.startDeckExport.mockResolvedValue(job({ state: 'queued' }));
    api.fetchDeckExport
      .mockResolvedValueOnce(job({ state: 'running', progress: 0.4 }))
      .mockResolvedValueOnce(job({ state: 'done', progress: 1, file: 'j1.pdf' }));
    const seen: number[] = [];
    const run = runDeckExport('m1', 'demo', 'pdf', { onProgress: (p) => seen.push(p) });
    await vi.advanceTimersByTimeAsync(DECK_EXPORT_POLL_MS * 3);
    const done = await run;
    expect(done.state).toBe('done');
    expect(toast).toHaveBeenCalledWith('next.deck.menu.pdfToast');
    expect(seen).toEqual([0, 0.4, 1]);
    expect(clicked).toEqual(['/dl?artifact=demo&job=j1']);
  });

  it('asks for the guests copy of a PDF and for a recording of a video', async () => {
    api.startDeckExport.mockResolvedValue(job({ state: 'done', progress: 1 }));
    await runDeckExport('m1', 'demo', 'pdf', { publish: true });
    expect(api.startDeckExport).toHaveBeenCalledWith('m1', 'demo', 'pdf', undefined, true);
    api.startDeckExport.mockResolvedValue(job({ format: 'mp4', state: 'done', progress: 1 }));
    await runDeckExport('m1', 'demo', 'mp4', { rec: 'r1' });
    expect(api.startDeckExport).toHaveBeenLastCalledWith('m1', 'demo', 'mp4', 'r1', undefined);
    expect(toast).toHaveBeenLastCalledWith('next.deck.toast.videoBuilding');
    // A video is picked up from the montage, not downloaded here.
    expect(clicked).toHaveLength(1);
  });

  it('says why a job failed, in the runtime words', async () => {
    api.startDeckExport.mockResolvedValue(job({ state: 'failed', error: 'Chrome is not installed' }));
    await expect(runDeckExport('m1', 'demo', 'pdf')).rejects.toThrow('Chrome is not installed');
    expect(toast).toHaveBeenLastCalledWith('next.deck.menu.failed', {
      tone: 'error',
      description: 'Chrome is not installed',
    });
    expect(clicked).toEqual([]);
  });

  it('calls the export lost after three polls fail in a row, not after one', async () => {
    api.fetchDeckExport
      .mockRejectedValueOnce(new Error('network'))
      .mockResolvedValueOnce(job({ state: 'running' }))
      .mockRejectedValue(new Error('gone'));
    const run = followDeckExport('m1', 'demo', job({}));
    const caught = run.catch((e: Error) => e.message);
    await vi.advanceTimersByTimeAsync(DECK_EXPORT_POLL_MS * 6);
    expect(await caught).toBe('gone');
    expect(api.fetchDeckExport).toHaveBeenCalledTimes(5);
  });

  it('stops quietly when aborted', async () => {
    api.startDeckExport.mockResolvedValue(job({ state: 'running' }));
    const ctl = new AbortController();
    const run = runDeckExport('m1', 'demo', 'pdf', { signal: ctl.signal });
    const caught = run.catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(10);
    ctl.abort();
    await vi.advanceTimersByTimeAsync(10);
    expect(await caught).toBeDefined();
    expect(toast).toHaveBeenCalledTimes(1);
  });
});
