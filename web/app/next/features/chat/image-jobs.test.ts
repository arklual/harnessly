import { describe, expect, it } from 'vitest';
import type { ImageGen } from '@/next/protocol/v2';
import { cardView, columnsOf, followsJob, jobOf, type IImageJob } from '@/next/features/chat/image-jobs';

const images = (over: Partial<ImageGen> = {}): ImageGen => ({
  count: 3,
  prompt: 'a fox',
  shape: 'landscape',
  jobId: '',
  state: 'drawing',
  done: [],
  failed: null,
  ...over,
});
const job = (over: Partial<IImageJob> = {}): IImageJob => ({
  id: 'j1',
  prompt: 'a fox',
  count: 3,
  state: 'running',
  paths: [],
  error: null,
  startedAt: 1000,
  ...over,
});

describe('the picture card', () => {
  it('finds the job of a pending call by its brief and start, and of a later call by its id', () => {
    const jobs = [job({ id: 'old', startedAt: 1500 - 60_000 }), job({ id: 'mine', startedAt: 2000 })];
    expect(jobOf(jobs, images(), 1500)?.id).toBe('mine');
    expect(jobOf(jobs, images({ jobId: 'old' }), 1500)?.id).toBe('old');
    expect(jobOf(jobs, images({ prompt: 'a cat' }), 1500)).toBeNull();
  });

  it('shows a tile per picture asked for while drawing, the ready ones filled in', () => {
    expect(cardView(images(), job({ paths: ['/a.png'] })).tiles).toEqual(['/a.png', null, null]);
  });

  it('shows only the pictures there are once the call ended, and the failure', () => {
    const view = cardView(images({ state: 'done', done: ['/a.png'], failed: 'refused' }), null);
    expect(view).toEqual({ tiles: ['/a.png'], drawing: false, failed: 'refused' });
  });

  it('lays tiles out in one, two or three columns', () => {
    expect([1, 2, 3, 4, 5, 8].map(columnsOf)).toEqual([1, 2, 2, 2, 3, 3]);
  });

  it('follows the jobs of harnessly tools only', () => {
    expect(followsJob('mcp__harnessly__generate_images')).toBe(true);
    expect(followsJob('mcp__image_gen__imagegen')).toBe(false);
  });
});
