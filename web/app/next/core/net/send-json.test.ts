import { afterEach, describe, expect, it } from 'vitest';
import { setFetch, sendJson } from '@/next/core/net/http';

const reply = (status: number, body: unknown) =>
  new Response(body === null ? null : JSON.stringify(body), { status });

afterEach(() => setFetch((url, init) => fetch(url, init)));

describe('sendJson', () => {
  it('sends the body as a PATCH and reads the answer', async () => {
    const seen: { url: string; init: RequestInit }[] = [];
    setFetch(async (url, init) => {
      seen.push({ url, init });
      return reply(200, { n: 2 });
    });
    const res = await sendJson('/x', { a: 1 }, (v) => (v as { n: number }).n, 1000);
    expect(res).toEqual({ ok: true, value: 2 });
    expect(seen[0]?.init.method).toBe('PATCH');
    expect(seen[0]?.init.body).toBe('{"a":1}');
  });

  it('takes `accepted` as the answer of a change that was taken, not as a refusal', async () => {
    setFetch(async () => reply(200, { r: 'accepted', me: { rev: 3 } }));
    const res = await sendJson('/x', {}, (v) => (v as { me: { rev: number } }).me.rev, 1000);
    expect(res).toEqual({ ok: true, value: 3 });
  });

  it('gives a refusal as the refusal it is', async () => {
    setFetch(async () => reply(409, { r: 'refused', code: 'conflict' }));
    expect(await sendJson('/x', {}, (v) => v, 1000)).toEqual({ r: 'refused', code: 'conflict' });
  });

  it('fails with the network, and with the status of an answer that is not JSON', async () => {
    setFetch(async () => {
      throw new Error('down');
    });
    expect(await sendJson('/x', {}, (v) => v, 1000)).toEqual({ r: 'failed', code: 'network', status: null });
    setFetch(async () => new Response('<html>', { status: 502 }));
    expect(await sendJson('/x', {}, (v) => v, 1000)).toEqual({ r: 'failed', code: 'http', status: 502 });
  });
});
