import { afterEach, describe, expect, it, vi } from 'vitest';

import { liveOrgApi } from '../live';

const answer = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

afterEach(() => vi.unstubAllGlobals());

describe('a machine the hub does not trust yet', () => {
  it('reads the hub refusal as «untrusted» and lets the owner trust the machine', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith('/open')) return answer(403, { error: 'machine_untrusted', message: 'нужно доверие' });
      expect(init?.method).toBe('POST');
      return answer(200, { machine: { id: 'm1' } });
    });
    vi.stubGlobal('fetch', fetchMock);
    const api = liveOrgApi();
    await expect(api.openMachine('m1')).rejects.toMatchObject({ code: 'untrusted' });
    await api.trustMachine('m1');
    expect(String(fetchMock.mock.calls.at(-1)?.[0])).toBe('/api/machines/m1/trust');
  });
});

describe('creating an organization', () => {
  it('sends the first tenant name along', async () => {
    const fetchMock = vi.fn(async () => answer(201, { org: { slug: 'acme', name: 'Acme' }, tenants: [] }));
    vi.stubGlobal('fetch', fetchMock);
    const { slug } = await liveOrgApi().createOrg({ name: 'Acme', spaceIds: [], tenantName: 'Продажи' });
    expect(slug).toBe('acme');
    const init = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect(JSON.parse(String(init.body))).toMatchObject({ name: 'Acme', tenantName: 'Продажи' });
  });
});
