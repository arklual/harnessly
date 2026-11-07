import { afterEach, describe, expect, it } from 'vitest';
import { setFetch } from '@/next/core/net/http';
import {
  DEFAULT_SETTINGS,
  httpMeSource,
  loadMe,
  localMeSource,
  meStore,
  setMeSource,
  type Me,
} from '@/next/stores/me';

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

describe('the contract of /api/v2/me (GitHub #29)', () => {
  it('a 200 without settings is a failure, not a person, and never throws', async () => {
    setFetch(async () => reply(200, { email: 'a@example.com', name: 'Ann', rev: 3 }));
    expect(await httpMeSource().load()).toEqual({ k: 'failed', code: 'http' });
    setFetch(async () => reply(200, { email: 'a@example.com', settings: { ...DEFAULT_SETTINGS } }));
    expect(await httpMeSource().load()).toEqual({ k: 'failed', code: 'http' });
    setFetch(async () => reply(200, '<html>'));
    expect(await httpMeSource().load()).toEqual({ k: 'failed', code: 'http' });
  });

  it('a setting an older runtime does not send yet is the default; one of the wrong type is too', async () => {
    setFetch(async () =>
      reply(200, { email: 'a@example.com', name: null, rev: 2, settings: { enterSends: false, density: 7 } }),
    );
    const st = await httpMeSource().load();
    expect(st).toEqual({
      k: 'ready',
      me: {
        email: 'a@example.com',
        name: null,
        rev: 2,
        settings: { ...DEFAULT_SETTINGS, enterSends: false },
      },
    });
  });

  it('a refused or broken connection is a failure; 401 stays the sign-in screen', async () => {
    setFetch(async () => {
      throw new Error('reset');
    });
    expect(await httpMeSource().load()).toEqual({ k: 'failed', code: 'network' });
    setFetch(async () => reply(502, { error: 'bad gateway' }));
    expect(await httpMeSource().load()).toEqual({ k: 'failed', code: 'http' });
    setFetch(async () => reply(401, { error: 'unauthorized' }));
    expect(await httpMeSource().load()).toEqual({ k: 'signed-out' });
  });

  it('loadMe turns a source that throws into a failure', async () => {
    setMeSource({
      load: async () => {
        throw new TypeError("Cannot read properties of undefined (reading 'density')");
      },
      patch: async () => ({ r: 'failed' }),
    });
    expect(await loadMe()).toEqual({ k: 'failed', code: 'http' });
    expect(meStore.get()).toEqual({ k: 'failed', code: 'http' });
    setMeSource(localMeSource());
  });
});
