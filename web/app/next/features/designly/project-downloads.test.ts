// «Скачать» of a Sinopia project: the page on screen first (PDF, one HTML, PNG), then every page, then the whole
// project for those who may change it; each item asks the machine for its job with the page it is of.
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';

const runDeckExport = vi.fn(() => Promise.resolve({}));
vi.mock('@/next/features/designly/deck/exports', () => ({ runDeckExport }));

const { DOWNLOAD_UNIT, projectDownloads } = await import('@/next/features/designly/project-downloads');

beforeAll(() => pinLocale('en'));
beforeEach(() => runDeckExport.mockClear());

const of = { machine: 'm1', artifact: 'demo', board: 'Home.dc.html', archives: true } as const;

describe('the downloads of a project', () => {
  it('offers the page, every page and the whole project, in this order', () => {
    expect(projectDownloads(of).map((i) => i.id)).toEqual([
      'page-pdf',
      'page-html',
      'page-png',
      'all-pdf',
      'bundle',
    ]);
    expect(projectDownloads({ ...of, board: null, archives: false }).map((i) => i.id)).toEqual(['all-pdf']);
    expect(projectDownloads(of, DOWNLOAD_UNIT.scene)[0].label).toBe('This scene — PDF');
  });

  it('asks for the job of the page it is of', () => {
    const items = projectDownloads(of);
    items.find((i) => i.id === 'page-html')?.pick();
    expect(runDeckExport).toHaveBeenLastCalledWith('m1', 'demo', 'html', { board: 'Home.dc.html' });
    items.find((i) => i.id === 'bundle')?.pick();
    expect(runDeckExport).toHaveBeenLastCalledWith('m1', 'demo', 'bundle', { board: undefined });
  });
});
