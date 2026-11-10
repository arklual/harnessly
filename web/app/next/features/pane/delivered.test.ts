// HRN-879: under a hub a file the agent handed over is saved from the hub's store by its sha256, so the card works with
// the machine off; without a store, or without the hash, from the machine as before.
import { afterEach, describe, expect, it, vi } from 'vitest';

const config = vi.hoisted(() => ({ centralUploads: true, platformApi: '/api/acme' }));
vi.mock('@/platform/config', () => ({ appConfig: () => config, platformApi: () => config.platformApi }));

import { downloadUrl, previewUrl } from '@/next/features/pane/delivered';
import { connectionStore } from '@/next/stores/connection';

const SHA = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad';
const file = { sha256: SHA, mime: 'application/pdf', name: 'отчёт.pdf' };

describe('a handed-over file', () => {
  afterEach(() => {
    config.centralUploads = true;
  });

  it('is saved from the hub by its hash', () => {
    const url = new URL(downloadUrl('m-1' as never, '/home/u/report.pdf', file), 'http://x');
    expect(url.pathname).toBe(`/api/acme/uploads/delivered/${SHA}`);
    expect(Object.fromEntries(url.searchParams)).toEqual({
      machine: 'm-1',
      path: '/home/u/report.pdf',
      type: 'application/pdf',
      name: 'отчёт.pdf',
      download: '1',
    });
  });

  it('is saved from the machine without a store or a hash', () => {
    const machine = '/api/acme/file-preview?download=1&path=%2Fhome%2Fu%2Freport.pdf&machine=m-1';
    expect(downloadUrl('m-1' as never, '/home/u/report.pdf', { ...file, sha256: 'nope' })).toBe(machine);
    config.centralUploads = false;
    expect(downloadUrl('m-1' as never, '/home/u/report.pdf', file)).toBe(machine);
  });

  it('is shown in the pane from the hub while the machine is not live', () => {
    connectionStore.set('m-1' as never, { k: 'live' } as never);
    expect(previewUrl('m-1' as never, '/home/u/report.pdf')).toBe(
      '/api/acme/file-preview?path=%2Fhome%2Fu%2Freport.pdf&machine=m-1',
    );
    connectionStore.set('m-1' as never, { k: 'offline' } as never);
    expect(previewUrl('m-1' as never, '/home/u/report.pdf')).toBe(
      '/api/acme/uploads/delivered/by-path?machine=m-1&path=%2Fhome%2Fu%2Freport.pdf',
    );
  });
});
