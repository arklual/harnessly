import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, expect, it } from 'vitest';

import type { OrgList, OrgView } from '../../hub-api/orgs';
import type { Fetcher } from '../lib/api';
import type { Config } from '../lib/types';
import { OrgApiError, orgErrorText } from './api/errors';
import { liveTenant } from './api/live/tenant';
import { tenantMock } from './mock/tenant';
import { orgAddress, orgOfSpace, orgTargetFor, sectionForSpaceRoute, type TenantLookup } from './redirect';

const config = (orgs: string) => ({ orgs }) as unknown as Config;

const lookup = (opts: {
  loose?: string[];
  tenants?: Record<string, string[]>;
  fail?: boolean;
}): TenantLookup => ({
  list: () =>
    opts.fail
      ? Promise.reject(new Error('down'))
      : Promise.resolve({
          orgs: Object.keys(opts.tenants ?? {}).map((slug) => ({ slug })),
          looseSpaces: (opts.loose ?? []).map((slug) => ({ slug })),
        } as unknown as OrgList),
  view: (org) =>
    Promise.resolve({
      org: { slug: org },
      tenants: (opts.tenants?.[org] ?? []).map((slug) => ({ slug })),
    } as unknown as OrgView),
});

describe('the old space address of a tenant', () => {
  it('maps home, settings and the machines step; nothing else', () => {
    expect(sectionForSpaceRoute('/s/:slug')).toBe('overview');
    expect(sectionForSpaceRoute('/s/:slug/settings')).toBe('teams');
    expect(sectionForSpaceRoute('/s/:slug/setup/:step', 'machines')).toBe('machines');
    expect(sectionForSpaceRoute('/s/:slug/setup/:step', 'agents')).toBeNull();
    expect(sectionForSpaceRoute('/s/:slug/sites/new')).toBeNull();
    expect(sectionForSpaceRoute('/s/:slug/sites/:name')).toBeNull();
  });

  it('builds the organization address of a tenant', () => {
    expect(orgAddress('acme', 'product', 'machines')).toBe('/o/acme/product/machines');
  });

  it('redirects a tenant of an organization to its section', async () => {
    const l = lookup({ tenants: { other: ['x'], acme: ['product', 'platform'] } });
    expect(await orgOfSpace('platform', l)).toBe('acme');
    expect(await orgTargetFor(config('on'), '/s/:slug', 'platform', undefined, l)).toBe(
      '/o/acme/platform/overview',
    );
    expect(await orgTargetFor(config('on'), '/s/:slug/settings', 'product', undefined, l)).toBe(
      '/o/acme/product/teams',
    );
    expect(await orgTargetFor(config('on'), '/s/:slug/setup/:step', 'product', 'machines', l)).toBe(
      '/o/acme/product/machines',
    );
  });

  it('keeps the old screens for a space with no organization', async () => {
    const l = lookup({ loose: ['solo'], tenants: { acme: ['product'] } });
    expect(await orgOfSpace('solo', l)).toBeNull();
    expect(await orgTargetFor(config('on'), '/s/:slug', 'solo', undefined, l)).toBeNull();
    expect(await orgTargetFor(config('on'), '/s/:slug', 'unknown', undefined, l)).toBeNull();
  });

  it('keeps the old screens when organizations are off, the route has no new place or the hub fails', async () => {
    const l = lookup({ tenants: { acme: ['product'] } });
    expect(await orgTargetFor(config('off'), '/s/:slug', 'product', undefined, l)).toBeNull();
    expect(await orgTargetFor(config('shadow'), '/s/:slug', 'product', undefined, l)).toBeNull();
    expect(await orgTargetFor(config('on'), '/s/:slug/sites/new', 'product', undefined, l)).toBeNull();
    expect(await orgTargetFor(config('on'), '/s/:slug/setup/:step', 'product', 'agents', l)).toBeNull();
    expect(
      await orgTargetFor(config('on'), '/s/:slug', 'product', undefined, lookup({ fail: true })),
    ).toBeNull();
  });
});

const sources = (dir: string): string[] =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return sources(path);
    return /\.(ts|tsx)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : [];
  });

describe('the organization hub', () => {
  it('links nowhere into the old space screens', () => {
    const here = fileURLToPath(new URL('.', import.meta.url));
    const bad = sources(here).flatMap((file) =>
      readFileSync(file, 'utf8')
        .split('\n')
        // A route written as a string or a template: '/s/…', "/s/…", `/s/…`, to="/s/…".
        .filter((line) => /(['"`])\/s\/[\w$]/.test(line))
        .map((line) => `${file}: ${line.trim()}`),
    );
    expect(bad).toEqual([]);
  });
});

interface Call {
  method: string;
  url: string;
  body: unknown;
}

const hub = (answer: (call: Call) => Response) => {
  const calls: Call[] = [];
  const fetcher: Fetcher = (input, init) => {
    const call: Call = {
      method: init?.method ?? 'GET',
      url: input,
      body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
    };
    calls.push(call);
    return Promise.resolve(answer(call));
  };
  return { calls, api: liveTenant(fetcher) };
};

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const invite = (over: Record<string, unknown> = {}) => ({
  id: 'i1',
  kind: 'email',
  email: 'a@acme.example',
  role: 'member',
  maxUses: 1,
  uses: 0,
  expiresAt: '2026-11-09T00:00:00Z',
  createdAt: '2026-11-02T00:00:00Z',
  ...over,
});

describe('machine codes', () => {
  const issued = { code: 'C1', expiresAt: '2026-11-02T12:00:00Z', profile: 'dir', command: 'curl x | sh' };

  it('asks the space endpoint for a code and hands back the hub command as it is', async () => {
    const { calls, api } = hub(() => json(issued));
    expect(await api.issueMachineCode('sp 1', 'dir', false)).toEqual(issued);
    expect(calls).toEqual([
      { method: 'POST', url: '/api/spaces/sp%201/machines/codes', body: { profile: 'dir' } },
    ]);
  });

  it('sends the confirmation only for the full profile', async () => {
    const { calls, api } = hub(() => json({ ...issued, profile: 'full' }));
    await api.issueMachineCode('sp1', 'full', true);
    expect(calls[0]?.body).toEqual({ profile: 'full', confirmFull: true });
  });

  it('says that containers are off on this hub', async () => {
    const { api } = hub(() => json({ error: 'provider_off', message: '' }, 422));
    const e = await api.issueMachineCode('sp1', 'container', false).catch((x: unknown) => x);
    expect(e).toBeInstanceOf(OrgApiError);
    expect(orgErrorText(e)).toMatch(/Контейнеры/);
  });

  it('shows the hub refusal of someone who is not the owner', async () => {
    const { api } = hub(() => json({ error: 'forbidden', message: '' }, 403));
    const e = await api.issueMachineCode('sp1', 'dir', false).catch((x: unknown) => x);
    expect(orgErrorText(e)).not.toBe('');
  });

  it('lists the machines of the space to see which one dialed in', async () => {
    const { calls, api } = hub(() =>
      json({ machines: [{ id: 'm1', name: 'build', status: 'online', hostname: 'h' }] }),
    );
    expect(await api.spaceMachines('sp1')).toEqual([{ id: 'm1', name: 'build', status: 'online' }]);
    expect(calls[0]).toMatchObject({ method: 'GET', url: '/api/spaces/sp1/machines' });
  });
});

describe('invitations', () => {
  it('invites by email, by link, lists and revokes', async () => {
    const { calls, api } = hub(({ method, url }) => {
      if (method === 'DELETE') return new Response(null, { status: 204 });
      if (url.endsWith('/invites/link'))
        return json({ invite: invite({ id: 'i2', kind: 'link', email: null, url: 'https://h/i/x' }) });
      if (method === 'POST') return json({ invites: [invite({ url: 'https://h/i/y', sent: false })] });
      return json({ invites: [invite()] });
    });
    const byMail = await api.inviteByEmail('sp1', 'a@acme.example');
    expect(byMail).toMatchObject({ id: 'i1', url: 'https://h/i/y', sent: false });
    const byLink = await api.inviteByLink('sp1');
    expect(byLink).toMatchObject({ kind: 'link', url: 'https://h/i/x' });
    expect((await api.tenantInvites('sp1'))[0]).toMatchObject({ id: 'i1', url: null });
    await api.revokeInvite('sp1', 'i1');
    expect(calls).toEqual([
      { method: 'POST', url: '/api/spaces/sp1/invites', body: { emails: ['a@acme.example'] } },
      { method: 'POST', url: '/api/spaces/sp1/invites/link', body: { days: 7 } },
      { method: 'GET', url: '/api/spaces/sp1/invites', body: undefined },
      { method: 'DELETE', url: '/api/spaces/sp1/invites/i1', body: undefined },
    ]);
  });
});

describe('tenant members and name', () => {
  it('changes a role, removes a member and renames the tenant on the space endpoints', async () => {
    const { calls, api } = hub(({ method, url }) => {
      if (method === 'DELETE') return new Response(null, { status: 204 });
      if (url.endsWith('/members')) {
        return json({
          members: [{ userId: 'u1', email: 'a@acme.example', name: 'Аня', role: 'owner', joinedAt: 'x' }],
        });
      }
      if (method === 'PATCH' && url === '/api/spaces/sp1') return json({ space: { name: 'Новое имя' } });
      return json({ member: {} });
    });
    expect(await api.tenantMembers('sp1')).toEqual([
      { userId: 'u1', email: 'a@acme.example', name: 'Аня', role: 'owner' },
    ]);
    await api.setTenantRole('sp1', 'u1', 'member');
    await api.removeTenantMember('sp1', 'u2');
    expect(await api.renameTenant('sp1', 'Новое имя')).toBe('Новое имя');
    expect(calls.slice(1)).toEqual([
      { method: 'PATCH', url: '/api/spaces/sp1/members/u1', body: { role: 'member' } },
      { method: 'DELETE', url: '/api/spaces/sp1/members/u2', body: undefined },
      { method: 'PATCH', url: '/api/spaces/sp1', body: { name: 'Новое имя' } },
    ]);
  });

  it('shows the hub refusal to take the last owner away', async () => {
    const { api } = hub(() =>
      json({ error: 'conflict', message: '', details: { reason: 'last_owner' } }, 409),
    );
    const e = await api.setTenantRole('sp1', 'u1', 'member').catch((x: unknown) => x);
    expect(orgErrorText(e)).toMatch(/владел/i);
  });
});

describe('mock mode', () => {
  it('offers the same flows with fake data', async () => {
    const api = tenantMock({ role: 'admin' });
    await expect(api.issueMachineCode('t-product', 'full', false)).rejects.toBeInstanceOf(OrgApiError);
    const code = await api.issueMachineCode('t-product', 'dir', false);
    expect(code.command).toContain(code.code);
    const link = await api.inviteByLink('t-product');
    expect(link.url).toBeTruthy();
    expect((await api.tenantInvites('t-product')).some((i) => i.id === link.id)).toBe(true);
    await api.revokeInvite('t-product', link.id);
    expect((await api.tenantInvites('t-product')).some((i) => i.id === link.id)).toBe(false);
    await expect(api.setTenantRole('t-product', 'u-andrey', 'member')).rejects.toBeInstanceOf(OrgApiError);
  });
});
