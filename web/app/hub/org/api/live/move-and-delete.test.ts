import { afterEach, describe, expect, it, vi } from 'vitest';

import { liveOrgApi } from '../live';

const answer = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

afterEach(() => vi.unstubAllGlobals());

const wirePlan = {
  from: { id: 'a', name: 'Старый' },
  to: { id: 'b', name: 'Новый' },
  noop: false,
  teamCleared: { kind: 'team', id: 't1', label: 'Продажи' },
  teamAssigned: null,
  accessRevoked: [{ kind: 'user', id: 'u1', label: 'Аня' }],
  limits: { fromRules: 0, toRules: 2 },
  kept: ['id'],
};

describe('moving a machine between tenants', () => {
  it('asks for the plan with dry_run and reads the labels', async () => {
    const fetchMock = vi.fn(async () => answer(200, { dry_run: true, plan: wirePlan }));
    vi.stubGlobal('fetch', fetchMock);
    const plan = await liveOrgApi().planMoveMachine('m1', 'b');
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/machines/m1/move');
    expect(JSON.parse(String(init.body))).toEqual({ tenantId: 'b', dry_run: true });
    expect(plan).toMatchObject({
      teamCleared: 'Продажи',
      accessRevoked: ['Аня'],
      limits: { fromRules: 0, toRules: 2 },
    });
  });

  it('moves without dry_run and shows the hub refusal as is', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(answer(200, { moved: true, plan: wirePlan }))
      .mockResolvedValueOnce(answer(409, { error: 'conflict', message: 'В тенанте нет места.' }));
    vi.stubGlobal('fetch', fetchMock);
    const api = liveOrgApi();
    await api.moveMachine('m1', 'b');
    const init = (fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1];
    expect(JSON.parse(String(init.body))).toEqual({ tenantId: 'b' });
    await expect(api.moveMachine('m1', 'b')).rejects.toMatchObject({
      code: 'hub',
      message: 'В тенанте нет места.',
    });
  });
});

describe('deleting a tenant', () => {
  it('reads the blockers of a dry run, then deletes', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        answer(200, {
          dry_run: true,
          plan: { blockers: [{ kind: 'machines', count: 2 }], removes: null, canDelete: false },
        }),
      )
      .mockResolvedValueOnce(
        answer(200, { deleted: true, plan: { blockers: [], removes: [], canDelete: true } }),
      );
    vi.stubGlobal('fetch', fetchMock);
    const api = liveOrgApi();
    const plan = await api.planDeleteTenant('acme', 'sp1');
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe('/api/orgs/acme/tenants/sp1?dry_run=true');
    expect(plan).toEqual({ blockers: [{ kind: 'machines', count: 2 }], removes: [], canDelete: false });
    await api.deleteTenant('acme', 'sp1');
    const [url, init] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(url).toBe('/api/orgs/acme/tenants/sp1');
    expect(init.method).toBe('DELETE');
  });
});
