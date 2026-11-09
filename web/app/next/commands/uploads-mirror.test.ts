// under a hub what a person attaches goes into the hub's store too, and the chat's uploads are read from the
// hub; elsewhere (a runtime on its own, a cluster) nothing changes.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const config = vi.hoisted(() => ({ centralUploads: true, platformApi: '/api/acme' }));
vi.mock('@/platform/config', () => ({ appConfig: () => config, platformApi: () => config.platformApi }));

import { keepInHub, mirrorUpload, takeKept, uploadSrc } from '@/next/commands/uploads';

const ref = {
  id: '/home/u/.harnessly/uploads/up_ABCDEFGH.png',
  name: 'a.png',
  kind: 'image',
  mime: 'image/png',
} as const;
const SHA_OF_ABC = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad';

describe('the chat uploads in the hub', () => {
  let calls: { url: string; method: string; body: unknown }[];
  beforeEach(() => {
    config.centralUploads = true;
    calls = [];
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string, init: RequestInit) => {
        calls.push({ url, method: init.method ?? 'GET', body: init.body });
        return new Response(null, { status: 204 });
      }),
    );
  });
  afterEach(() => vi.unstubAllGlobals());

  it('reads an upload from the hub, and from the machine without one', () => {
    expect(uploadSrc('m-1' as never, ref)).toBe('/api/acme/uploads/files/up_ABCDEFGH?machine=m-1');
    config.centralUploads = false;
    expect(uploadSrc('m-1' as never, ref)).toBe('/api/v2/uploads/up_ABCDEFGH?machine=m-1');
  });

  it('puts the bytes under their sha256, then names the machine upload they are', async () => {
    expect(await mirrorUpload('m-1' as never, new Blob(['abc']), ref as never)).toBe(true);
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
      `PUT /api/acme/uploads/blobs/${SHA_OF_ABC}?machine=m-1`,
      'PUT /api/acme/uploads/refs/up_ABCDEFGH?machine=m-1',
    ]);
    expect(JSON.parse(calls[1].body as string)).toEqual({
      sha256: SHA_OF_ABC,
      mime: 'image/png',
      name: 'a.png',
      size: 3,
    });
  });

  it('puts nothing anywhere but under a hub, and nothing that is not a machine upload', async () => {
    expect(
      await mirrorUpload('m-1' as never, new Blob(['abc']), {
        ...ref,
        id: 'data:image/png;base64,AAAA',
      } as never),
    ).toBe(false);
    config.centralUploads = false;
    expect(await mirrorUpload('m-1' as never, new Blob(['abc']), ref as never)).toBe(false);
    expect(calls).toEqual([]);
  });

  it('keeps a file of a machine that is off in the hub, and gives the machine its copy when it is back', async () => {
    const file = Object.assign(new Blob(['abc'], { type: 'image/png' }), { name: 'a.png' });
    const kept = await keepInHub('m-1' as never, file, () => undefined);
    expect(kept).toEqual({
      ok: true,
      ref: { id: `hub:${SHA_OF_ABC}`, name: 'a.png', kind: 'image', mime: 'image/png' },
    });
    expect(uploadSrc('m-1' as never, { id: `hub:${SHA_OF_ABC}`, mime: 'image/png' })).toBe(
      `/api/acme/uploads/blobs/${SHA_OF_ABC}?machine=m-1&type=image%2Fpng`,
    );
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
      `PUT /api/acme/uploads/blobs/${SHA_OF_ABC}?machine=m-1`,
    ]);
  });

  it('holds the message while the hub cannot give the file back', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(null, { status: 502 })),
    );
    expect(
      await takeKept('m-1' as never, [
        { id: `hub:${SHA_OF_ABC}`, name: 'a.png', kind: 'image', mime: 'image/png' },
      ]),
    ).toBeNull();
    const plain = { id: 'up_QRSTUVWXYZ234567', name: 'b.txt', kind: 'file', mime: 'text/plain' } as const;
    expect(await takeKept('m-1' as never, [plain])).toEqual([plain]);
  });
});
