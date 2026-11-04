// @vitest-environment jsdom
import { afterEach, describe, expect, it } from 'vitest';
import { setFetch } from '@/next/core/net/http';
import { grantAsk, patchSecret, putAsk } from '@/next/features/safe/api';
import type { MachineId } from '@/next/protocol/v2';

const MACHINE = 'local' as MachineId;
const json = (body: unknown) =>
  new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
afterEach(() => setFetch((url, init) => fetch(url, init)));

describe('safe ask actions', () => {
  it('encodes the ask ID and carries a value only in the ask-specific put body', async () => {
    const calls: { url: string; init: RequestInit }[] = [];
    setFetch(async (url, init) => {
      calls.push({ url, init });
      return json({ ready: 'ask/1', value: 'ignored' });
    });
    expect(await putAsk(MACHINE, 'ask/1', 'test value')).toEqual({ ok: true, value: 'ask/1' });
    expect(await grantAsk(MACHINE, 'ask/1')).toEqual({ ok: true, value: 'ask/1' });
    expect(calls.map((call) => call.url)).toEqual([
      '/api/platform/secret-asks/ask%2F1/put?machine=local',
      '/api/platform/secret-asks/ask%2F1/grant?machine=local',
    ]);
    expect(calls[0]!.init.method).toBe('POST');
    expect(JSON.parse(String(calls[0]!.init.body))).toEqual({ value: 'test value' });
    expect(calls[1]!.init.method).toBe('POST');
    expect(calls[1]!.init.body).toBeUndefined();
  });

  it('keeps grant and metadata refusals available to the UI', async () => {
    setFetch(async () => json({ error: 'not-found' }));
    expect(await grantAsk(MACHINE, 'gone')).toEqual({ ok: true, value: { refused: 'not-found' } });
    expect(await patchSecret(MACHINE, 'gone', { origins: [], projects: [] })).toEqual({
      ok: true,
      value: { refused: 'not-found' },
    });
  });
});
