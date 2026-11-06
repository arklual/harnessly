import { readFileSync } from 'node:fs';

import { afterEach, describe, expect, it, vi } from 'vitest';

import * as es from '../../../../hub-api/env.schema';
import { OrgApiError, orgErrorText } from '../errors';
import { liveEnv, scopeQuery } from './env';

const fixture = (name: string): unknown =>
  JSON.parse(readFileSync(new URL(`../../../../hub-api/fixtures/env/${name}.json`, import.meta.url), 'utf8'));

interface Seen {
  method: string;
  url: string;
  body: unknown;
}

/** A fetch that answers with `body` and remembers what it was asked. */
const answer = (status: number, body: unknown): Seen[] => {
  const seen: Seen[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init: RequestInit) => {
      seen.push({
        method: init.method ?? 'GET',
        url,
        body: typeof init.body === 'string' ? JSON.parse(init.body) : undefined,
      });
      return Promise.resolve(new Response(status === 204 ? null : JSON.stringify(body), { status }));
    }),
  );
  return seen;
};

afterEach(() => vi.unstubAllGlobals());

describe('the env fixtures are what the screen reads', () => {
  it('parse as the hub writes them', () => {
    expect(es.envResponse.check(fixture('env-admin'), '$')).toEqual([]);
    expect(es.envCard.check(fixture('card'), '$')).toEqual([]);
    expect(es.catalogResponse.check(fixture('catalog'), '$')).toEqual([]);
    expect(es.meEnvResponse.check(fixture('me'), '$')).toEqual([]);
  });
});

describe('scopeQuery', () => {
  it('leaves the scope out for all the tenants and names a tenant otherwise', () => {
    expect(scopeQuery('all')).toEqual({ scope: undefined });
    expect(scopeQuery('product')).toEqual({ scope: 'product' });
  });
});

describe('liveEnv', () => {
  it('reads the screen of a tenant and of all the tenants', async () => {
    const seen = answer(200, fixture('env-admin'));
    const view = await liveEnv().envView('acme', 'product');
    await liveEnv().envView('acme', 'all');
    expect(seen.map((s) => s.url)).toEqual(['/api/orgs/acme/env?scope=product', '/api/orgs/acme/env']);
    expect(view.layers[0]?.layer).toBe('org');
  });

  it('reads the card of an entry', async () => {
    const seen = answer(200, fixture('card'));
    const card = await liveEnv().envCard('acme', 'entry-org-review');
    expect(seen[0]?.url).toBe('/api/orgs/acme/env/items/entry-org-review');
    expect(card.bars14).toHaveLength(14);
  });

  it('adds, changes and removes an entry', async () => {
    const row = (fixture('env-admin') as { layers: { items: unknown[] }[] }).layers[0]?.items[0];
    const seen = answer(201, row);
    await liveEnv().envAdd('acme', { layer: 'tenant', scopeId: 't1', itemId: 'i1' });
    await liveEnv().envPatch('acme', 'e1', { enabled: false });
    answer(204, null);
    const gone = answer(204, null);
    await liveEnv().envRemove('acme', 'e1');
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual([
      'POST /api/orgs/acme/env/entries',
      'PATCH /api/orgs/acme/env/entries/e1',
    ]);
    expect(seen[0]?.body).toEqual({ layer: 'tenant', scopeId: 't1', itemId: 'i1' });
    expect(seen[1]?.body).toEqual({ enabled: false });
    expect(gone[0]).toMatchObject({ method: 'DELETE', url: '/api/orgs/acme/env/entries/e1' });
  });

  it('reads the catalog for the layer that adds from it', async () => {
    const seen = answer(200, fixture('catalog'));
    const items = await liveEnv().envCatalog('acme', { layer: 'team', scopeId: 'tm1' }, 'mcp');
    expect(seen[0]?.url).toBe('/api/orgs/acme/catalog?kind=mcp&layer=team&scopeId=tm1');
    expect(items.length).toBeGreaterThan(0);
  });

  it('reads and switches a person’s own environment', async () => {
    const seen = answer(200, fixture('me'));
    const mine = await liveEnv().envMine('product');
    const item = mine.items[0];
    await liveEnv().envMineSet('item-tracker', true, 'product');
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual([
      'GET /api/me/env?space=product',
      'PUT /api/me/env/item-tracker',
    ]);
    expect(seen[1]?.body).toEqual({ off: true, space: 'product' });
    expect(item?.canToggle).toBe(true);
  });

  it('changes the groups of a layer and a person’s own choice', async () => {
    const g = { id: 'g1', name: 'Бэкенд', hint: '', required: false, defaultOn: false, position: 0 };
    const seen = answer(200, { group: g });
    expect(await liveEnv().envGroupAdd('acme', { layer: 'org', scopeId: 'o1', name: 'Бэкенд' })).toEqual(g);
    await liveEnv().envGroupPatch('acme', 'g1', { required: true });
    await liveEnv().envMineGroup('g1', true, 'product');
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual([
      'POST /api/orgs/acme/env/groups',
      'PATCH /api/orgs/acme/env/groups/g1',
      'PUT /api/me/env/groups/g1',
    ]);
    expect(seen[2]?.body).toEqual({ on: true, space: 'product' });
  });

  it('changes the presets of a layer and a person picks one', async () => {
    const p = { id: 'p1', name: 'Разработчик', hint: '', groupIds: ['g1'], position: 0 };
    const seen = answer(200, { preset: p });
    expect(
      await liveEnv().envPresetAdd('acme', { layer: 'org', scopeId: 'o1', name: 'Разработчик' }),
    ).toEqual(p);
    await liveEnv().envPresetPatch('acme', 'p1', { groupIds: ['g1'] });
    await liveEnv().envPresetRemove('acme', 'p1');
    await liveEnv().envMinePreset('p1', 'product');
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual([
      'POST /api/orgs/acme/env/presets',
      'PATCH /api/orgs/acme/env/presets/p1',
      'DELETE /api/orgs/acme/env/presets/p1',
      'PUT /api/me/env/presets/p1',
    ]);
    expect(seen[3]?.body).toEqual({ space: 'product' });
  });

  it('applies a layer on its machines', async () => {
    const seen = answer(200, { machines: 3, started: 2, waiting: 1 });
    const out = await liveEnv().envApply('acme', { layer: 'tenant', scopeId: 't1', when: 'now' });
    expect(out).toEqual({ machines: 3, started: 2, waiting: 1 });
    expect(seen.map((s) => `${s.method} ${s.url}`)).toEqual(['POST /api/orgs/acme/env/apply']);
    expect(seen[0]?.body).toEqual({ layer: 'tenant', scopeId: 't1', when: 'now' });
  });

  it('turns the hub’s refusals into the codes the screen reads', async () => {
    const cases: [number, string, string][] = [
      [409, 'exists', 'Это уже есть в окружении.'],
      [409, 'locked', 'Закреплено выше: здесь это не изменить.'],
      [404, 'not_enabled', 'Окружение для этой организации не включено.'],
    ];
    for (const [status, code, text] of cases) {
      answer(status, { error: code, message: '' });
      const err = await liveEnv()
        .envPatch('acme', 'e1', { enabled: false })
        .catch((e: unknown) => e);
      expect(err).toBeInstanceOf(OrgApiError);
      expect((err as OrgApiError).code).toBe(code);
      expect(orgErrorText(err)).toBe(text);
    }
  });
});
