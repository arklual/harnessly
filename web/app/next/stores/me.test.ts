import { afterEach, describe, expect, it } from 'vitest';
import { setFetch } from '@/next/core/net/http';
import { DEFAULT_SETTINGS, httpMeSource, localMeSource, type Me } from '@/next/stores/me';

const me = (rev: number, patch: Partial<Me['settings']> = {}): Me => ({
  email: 'a@example.com',
  name: 'Ann',
  settings: { ...DEFAULT_SETTINGS, ...patch },
  rev,
});

const reply = (status: number, body: unknown) => new Response(JSON.stringify(body), { status });

afterEach(() => setFetch((url, init) => fetch(url, init)));

describe('httpMeSource.patch', () => {
  it('writes to PATCH /api/v2/me/settings with the revision and takes the record it answers', async () => {
    const seen: { url: string; body: unknown }[] = [];
    setFetch(async (url, init) => {
      seen.push({ url, body: JSON.parse(String(init.body)) });
      return reply(200, { r: 'accepted', me: me(5, { enterSends: false }) });
    });
    const res = await httpMeSource().patch(4, { enterSends: false });
    expect(res).toEqual({ r: 'accepted', me: me(5, { enterSends: false }) });
    expect(seen[0]?.url).toMatch(/\/api\/v2\/me\/settings$/);
    expect(seen[0]?.body).toEqual({ rev: 4, patch: { enterSends: false } });
  });

  it('reports a conflict and a bad request as refusals, anything else as a failure', async () => {
    setFetch(async () => reply(409, { r: 'refused', code: 'conflict' }));
    expect(await httpMeSource().patch(1, {})).toEqual({ r: 'refused', code: 'conflict' });
    setFetch(async () => reply(400, { r: 'refused', code: 'bad-request' }));
    expect(await httpMeSource().patch(1, {})).toEqual({ r: 'refused', code: 'bad-request' });
    setFetch(async () => reply(403, { r: 'refused', code: 'forbidden' }));
    expect(await httpMeSource().patch(1, {})).toEqual({ r: 'failed' });
    setFetch(async () => {
      throw new Error('down');
    });
    expect(await httpMeSource().patch(1, {})).toEqual({ r: 'failed' });
  });

  it('falls back to the local stand-in where the runtime does not serve the route', async () => {
    setFetch(async () => reply(404, { r: 'refused', code: 'not-found' }));
    const res = await httpMeSource(localMeSource({ rev: 1 })).patch(1, { textSize: 'l' });
    expect(res).toMatchObject({ r: 'accepted', me: { rev: 2, settings: { textSize: 'l' } } });
  });
});
