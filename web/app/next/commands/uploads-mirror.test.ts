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

class HubXhr {
  static requests: HubXhr[] = [];
  static onSend: ((request: HubXhr) => void) | null = null;
  upload: { onprogress: ((event: { loaded: number }) => void) | null } = { onprogress: null };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onabort: (() => void) | null = null;
  status = 0;
  responseText = '';
  timeout = 0;
  aborted = false;
  method = '';
  url = '';
  body: unknown;
  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader() {}
  getResponseHeader() {
    return null;
  }
  send(body: unknown) {
    this.body = body;
    HubXhr.requests.push(this);
    HubXhr.onSend?.(this);
  }
  abort() {
    this.aborted = true;
    this.onabort?.();
  }
  answer(status: number) {
    this.status = status;
    this.onload?.();
  }
}

describe('a hub upload cannot wait forever', () => {
  beforeEach(() => {
    HubXhr.requests = [];
    HubXhr.onSend = null;
    vi.stubGlobal('XMLHttpRequest', HubXhr);
    // Simulate a silent network for both transports: only the XHR has an idle deadline today.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => new Promise(() => undefined)),
    );
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('ends a silent hub upload with a retryable failure', async () => {
    let outcome: unknown = null;
    void keepInHub('m-1', new Blob(['abc']), () => undefined).then((value) => {
      outcome = value;
    });
    await vi.waitFor(() => expect(HubXhr.requests.length || vi.mocked(fetch).mock.calls.length).toBe(1));
    await vi.advanceTimersByTimeAsync(61_000);
    expect(outcome).toMatchObject({ ok: false, code: 'network' });
  });

  it('honours removal while the hub is silent', async () => {
    const stop = new AbortController();
    let outcome: unknown = null;
    void keepInHub('m-1', new Blob(['abc']), () => undefined, stop.signal).then((value) => {
      outcome = value;
    });
    await vi.waitFor(() => expect(HubXhr.requests.length || vi.mocked(fetch).mock.calls.length).toBe(1));
    stop.abort();
    await vi.advanceTimersByTimeAsync(1);
    expect(outcome).toEqual({ ok: false, code: 'aborted' });
    expect(HubXhr.requests[0]?.aborted).toBe(true);
  });

  it('lets a progressing upload run longer than the inactivity deadline', async () => {
    let outcome: unknown = null;
    void keepInHub('m-1', new Blob(['abc']), () => undefined).then((value) => {
      outcome = value;
    });
    await vi.waitFor(() => expect(HubXhr.requests).toHaveLength(1));
    for (let sent = 1; sent <= 2; sent += 1) {
      await vi.advanceTimersByTimeAsync(45_000);
      HubXhr.requests[0].upload.onprogress?.({ loaded: sent });
    }
    expect(outcome).toBeNull();
    HubXhr.requests[0].answer(204);
    await vi.advanceTimersByTimeAsync(1);
    expect(outcome).toMatchObject({ ok: true, ref: { id: `hub:${SHA_OF_ABC}` } });
  });

  it('can retry after a 502 without losing the file', async () => {
    const file = new Blob(['abc']);
    const first = keepInHub('m-1', file, () => undefined);
    await vi.waitFor(() => expect(HubXhr.requests).toHaveLength(1));
    HubXhr.requests[0].answer(502);
    expect(await first).toMatchObject({ ok: false, code: 'http' });
    const retry = keepInHub('m-1', file, () => undefined);
    await vi.waitFor(() => expect(HubXhr.requests).toHaveLength(2));
    HubXhr.requests[1].answer(204);
    expect(await retry).toMatchObject({ ok: true, ref: { id: `hub:${SHA_OF_ABC}` } });
  });

  it('does not send a file removed before upload starts', async () => {
    const stop = new AbortController();
    stop.abort();
    expect(await keepInHub('m-1', new Blob(['abc']), () => undefined, stop.signal)).toEqual({
      ok: false,
      code: 'aborted',
    });
    expect(HubXhr.requests).toHaveLength(0);
    expect(fetch).not.toHaveBeenCalled();
  });
});

describe('the chat uploads in the hub', () => {
  let calls: { url: string; method: string; body: unknown }[];
  beforeEach(() => {
    config.centralUploads = true;
    calls = [];
    HubXhr.requests = [];
    HubXhr.onSend = (request) => {
      calls.push({ url: request.url, method: request.method, body: request.body });
      queueMicrotask(() => request.answer(204));
    };
    vi.stubGlobal('XMLHttpRequest', HubXhr);
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
