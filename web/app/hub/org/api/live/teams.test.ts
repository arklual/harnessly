import { readFileSync } from 'node:fs';

import { afterEach, describe, expect, it, vi } from 'vitest';

import type { Currency } from '../../../../hub-api/common';
import type { Member, OrgView } from '../../../../hub-api/orgs';
import { OrgApiError, orgErrorText } from '../errors';
import { liveTeams, buildTeamsView, personalScope, tenantOfPerson, type TeamsInput } from './teams';

const fixture = (name: string): unknown =>
  JSON.parse(readFileSync(new URL(`../../../../hub-api/fixtures/${name}.json`, import.meta.url), 'utf8'));
const base = fixture('orgs/view-admin') as unknown as OrgView;
const fixed = (fixture('orgs/members') as unknown as { members: Member[] }).members;
const USD: Currency = { code: 'USD', perUsd: 1, rateDate: '2026-11-01', source: 'none' };

const tenantA = base.tenants[0];
if (!tenantA) throw new Error('fixture has no tenant');
const tenantB = { ...tenantA, id: 't-b', slug: 'beta', name: 'Бета' };
const loose: Member = { ...fixed[0]!, userId: 'u-loose', name: 'Без команды', teams: [], grants: [] };
const manage = base.me.scopes[`tenant:${tenantA.id}`];
if (!manage) throw new Error('fixture has no tenant caps');

const twoTenants: OrgView = {
  ...base,
  tenants: [tenantA, tenantB],
  me: { ...base.me, scopes: { ...base.me.scopes, 'tenant:t-b': manage } },
};

const input = (over: Partial<TeamsInput> = {}): TeamsInput => ({
  view: twoTenants,
  scope: { kind: 'org', id: base.org.id },
  members: [...fixed, loose],
  rules: [],
  currency: USD,
  spend: null,
  now: Date.parse('2026-11-02T12:00:00Z'),
  ...over,
});

describe('the person’s tenant in the «all» view', () => {
  it('comes from their team', () => {
    expect(tenantOfPerson(input(), fixed[3]!.userId)).toBe(tenantA.id);
  });

  it('is the only tenant of the organization', () => {
    expect(tenantOfPerson(input({ view: base }), 'u-loose')).toBe(tenantA.id);
  });

  it('is the one tenant the lookup found for a person without a team', () => {
    const memberTenants = new Map([['u-loose', ['t-b']]]);
    expect(tenantOfPerson(input({ memberTenants }), 'u-loose')).toBe('t-b');
    expect(personalScope(input({ memberTenants }), 'u-loose')).toEqual({ kind: 'tenant', id: 't-b' });
  });

  it('is nobody’s when two tenants hold them: the controls hide and say where to go', () => {
    const memberTenants = new Map([['u-loose', [tenantA.id, 't-b']]]);
    const i = input({ memberTenants });
    expect(tenantOfPerson(i, 'u-loose')).toBeNull();
    expect(() => personalScope(i, 'u-loose')).toThrow(OrgApiError);
    const row = buildTeamsView(i)
      .teams.flatMap((t) => t.people)
      .find((p) => p.id === 'u-loose');
    expect(row?.can.remove).toBe(false);
    expect(row?.can.limit).toBe(false);
    expect(row?.openIn?.map((t) => t.slug)).toEqual([tenantA.slug, 'beta']);
  });

  it('keeps the controls of a person whose tenant is known', () => {
    const row = buildTeamsView(input({ memberTenants: new Map([['u-loose', ['t-b']]]) }))
      .teams.flatMap((t) => t.people)
      .find((p) => p.id === 'u-loose');
    expect(row?.can.remove).toBe(true);
    expect(row?.openIn).toBeUndefined();
  });
});

describe('what the screen may offer', () => {
  it('can invite and make teams from the «all» view, and says the hub keeps no team extras', () => {
    const v = buildTeamsView(input());
    expect(v.can).toEqual({ createTeam: true, invite: true });
    expect(v.supports.teamExtras).toBe(false);
  });

  it('says which tenant a team belongs to', () => {
    const v = buildTeamsView(input());
    expect(v.teams.filter((t) => t.id !== null).every((t) => t.tenantId === tenantA.id)).toBe(true);
  });
});

describe('team calls', () => {
  afterEach(() => vi.unstubAllGlobals());

  const hub = () => {
    const calls: { method: string; url: string; body: unknown }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init?: RequestInit) => {
        const method = init?.method ?? 'GET';
        calls.push({ method, url, body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined });
        if (method === 'DELETE' && url.includes('/teams/'))
          return Promise.resolve(new Response(null, { status: 204 }));
        if (method !== 'GET') return Promise.resolve(new Response('{}', { status: 200 }));
        if (url.includes('/members')) return Promise.resolve(Response.json({ members: fixed }));
        if (url.includes('/limits'))
          return Promise.resolve(Response.json({ rules: [], currency: USD, mode: 'off' }));
        if (url.includes('/spend'))
          return Promise.resolve(Response.json({ error: 'not_found' }, { status: 404 }));
        return Promise.resolve(Response.json(twoTenants));
      }),
    );
    return calls;
  };

  it('renames a team with PATCH', async () => {
    const calls = hub();
    await liveTeams().renameTeam('acme', 'all', 'm1', '  Новое имя ');
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch).toMatchObject({ url: '/api/orgs/acme/teams/m1', body: { name: 'Новое имя' } });
  });

  it('deletes a team with DELETE', async () => {
    const calls = hub();
    await liveTeams().deleteTeam('acme', 'all', 'm1');
    expect(calls.find((c) => c.method === 'DELETE')?.url).toBe('/api/orgs/acme/teams/m1');
  });

  it('makes a team in the tenant the form picked', async () => {
    const calls = hub();
    await liveTeams().createTeam('acme', 'all', 'Новая', 't-b');
    expect(calls.find((c) => c.method === 'POST')).toMatchObject({
      url: '/api/orgs/acme/teams',
      body: { tenantId: 't-b', name: 'Новая' },
    });
  });

  it('removes a person without a team from the tenant that holds them', async () => {
    const calls = hub();
    await liveTeams().removeFromTenant('acme', 'all', fixed[3]!.userId);
    expect(calls.find((c) => c.method === 'DELETE')?.url).toBe(
      `/api/spaces/${tenantA.id}/members/${fixed[3]!.userId}`,
    );
  });
});

describe('errors read in Russian', () => {
  it('never shows a raw English message', () => {
    expect(orgErrorText(new Error('machines per person: not supported'))).toBe(
      'Не получилось. Попробуйте ещё раз.',
    );
    expect(orgErrorText(new OrgApiError('invalid_request', 'team not found'))).toBe(
      'Не получилось. Попробуйте ещё раз.',
    );
  });

  it('keeps the hub’s own Russian text', () => {
    expect(orgErrorText(new OrgApiError('invalid_request', 'Назовите команду'))).toBe('Назовите команду');
  });
});
