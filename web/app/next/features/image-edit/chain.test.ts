import { describe, expect, it } from 'vitest';
import { chainVersions, type IChainJob } from '@/next/features/image-edit/chain';

const src = (path: string) => `/v?${path}`;
const original = { id: '/a.png', src: src('/a.png') };
const job = (over: Partial<IChainJob> & { startedAt: number }): IChainJob => ({
  state: 'done',
  paths: [],
  ...over,
});

describe('chainVersions', () => {
  it('is the original alone when nothing changed it', () => {
    expect(chainVersions(original, [], src)).toEqual([original]);
  });

  it('walks the chain, ordered by when the jobs started, whatever order they are listed in', () => {
    const jobs = [
      job({ startedAt: 30, source: '/b.png', paths: ['/c.png'], mode: 'erase' }),
      job({ startedAt: 10, source: '/a.png', paths: ['/b.png'], mode: 'mark' }),
    ];
    expect(chainVersions(original, jobs, src).map((v) => v.id)).toEqual(['/a.png', '/b.png', '/c.png']);
  });

  it('follows several changes of the same version', () => {
    const jobs = [
      job({ startedAt: 10, source: '/a.png', paths: ['/b.png'] }),
      job({ startedAt: 20, source: '/a.png', paths: ['/c.png'] }),
    ];
    expect(chainVersions(original, jobs, src).map((v) => v.id)).toEqual(['/a.png', '/b.png', '/c.png']);
  });

  it('skips jobs of other pictures, new pictures, running and failed jobs', () => {
    const jobs = [
      job({ startedAt: 1, source: '/other.png', paths: ['/x.png'] }),
      job({ startedAt: 2, paths: ['/new.png'] }),
      job({ startedAt: 3, source: '/a.png', paths: [], state: 'running' }),
      job({ startedAt: 4, source: '/a.png', paths: ['/f.png'], state: 'failed' }),
      job({ startedAt: 5, source: '/x.png', paths: ['/y.png'] }),
    ];
    expect(chainVersions(original, jobs, src)).toEqual([original]);
  });

  it('takes the last picture of a job and marks a cut as clear', () => {
    const jobs = [job({ startedAt: 1, source: '/a.png', paths: ['/b1.png', '/b2.png'], mode: 'cut' })];
    expect(chainVersions(original, jobs, src)[1]).toEqual({
      id: '/b2.png',
      src: src('/b2.png'),
      clear: true,
    });
  });

  it('a version already in the chain is not added twice', () => {
    const jobs = [job({ startedAt: 1, source: '/a.png', paths: ['/a.png'] })];
    expect(chainVersions(original, jobs, src)).toEqual([original]);
  });
});
