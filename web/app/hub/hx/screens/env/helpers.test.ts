import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import type { EnvEntryRow, EnvResponse, MeEnvItem } from '../../../../hub-api/env';
import {
  catalogTarget,
  dayBars,
  draftChanged,
  draftOf,
  draftPatch,
  draftValid,
  entryIn,
  findRow,
  flatRows,
  fmtNum,
  groupsOf,
  hasPatch,
  kindLong,
  kindShort,
  lastUsed,
  layerInitial,
  mineFiltered,
  mineSource,
  deliveryIssue,
  deliveryTile,
  plural,
  tilesOf,
  withAdded,
  withMineOff,
  withoutEntry,
  withPatch,
  withRow,
} from './helpers';

const admin = JSON.parse(
  readFileSync(new URL('../../../../hub-api/fixtures/env/env-admin.json', import.meta.url), 'utf8'),
) as EnvResponse;

const NOW = Date.parse('2026-11-03T12:00:00Z');

const row = (over: Partial<EnvEntryRow> = {}): EnvEntryRow => ({
  entryId: 'e1',
  itemId: 'i1',
  kind: 'skill',
  name: 'Ревью кода',
  hint: 'проверка перед MR',
  audience: { mode: 'tenant', teams: [] },
  pinned: false,
  locked: false,
  lockedReason: null,
  enabled: true,
  callsWeek: 10,
  spark14: [],
  lastUsedAt: null,
  unused: false,
  delivery: null,
  ...over,
});

const firstRow = (v: EnvResponse) => v.layers.flatMap((l) => l.items)[0]!;

describe('words', () => {
  it('formats numbers and plurals the way the board does', () => {
    expect(fmtNum(2390)).toBe('2 390');
    expect(plural(1, 'скилл', 'скилла', 'скиллов')).toBe('скилл');
    expect(plural(3, 'скилл', 'скилла', 'скиллов')).toBe('скилла');
    expect(plural(11, 'скилл', 'скилла', 'скиллов')).toBe('скиллов');
    expect(plural(21, 'скилл', 'скилла', 'скиллов')).toBe('скилл');
  });

  it('writes a layer line short and a tile line long', () => {
    expect(kindShort('skill', 3)).toBe('3 скилла');
    expect(kindShort('mcp', 2)).toBe('2 MCP');
    expect(kindShort('repo', 1)).toBe('1 репо');
    expect(kindShort('doc', 2)).toBe('2 док');
    expect(kindLong('repo', 3)).toBe('3 репы');
    expect(kindLong('doc', 3)).toBe('3 документа');
  });

  it('says when something was last called', () => {
    expect(lastUsed(null, NOW)).toBe('не вызывался');
    expect(lastUsed('2026-11-03T11:59:30Z', NOW)).toBe('сейчас');
    expect(lastUsed('2026-11-03T11:55:00Z', NOW)).toBe('5 мин назад');
    expect(lastUsed('2026-11-03T09:00:00Z', NOW)).toBe('3 ч назад');
    expect(lastUsed('2026-11-02T08:00:00Z', NOW)).toBe('вчера');
    expect(lastUsed('2026-09-30T12:00:00Z', NOW)).toBe('34 дня назад');
  });

  it('takes the second word of a title for the initial', () => {
    expect(layerInitial('Организация Акме')).toBe('А');
    expect(layerInitial('Команда Фронт')).toBe('Ф');
  });
});

describe('tilesOf', () => {
  it('makes four tiles from the summary', () => {
    const tiles = tilesOf(admin);
    expect(tiles.map((t) => t.label)).toEqual([
      'В окружении',
      'Вызовов за неделю',
      'Не используются',
      'Доехало до машин',
    ]);
    expect(tiles[0]!.value.startsWith(`${admin.summary.total}:`)).toBe(true);
  });

  it('says so plainly when there is nothing yet', () => {
    const empty: EnvResponse = {
      ...admin,
      summary: {
        ...admin.summary,
        total: 0,
        byKind: { skill: 0, mcp: 0, repo: 0, doc: 0 },
        callsWeek: null,
        unused: null,
      },
    };
    const tiles = tilesOf(empty);
    expect(tiles[0]!.value).toBe('пока ничего');
    expect(tiles[1]!.value).toBe('данных о вызовах ещё нет');
    expect(tiles[2]!.value).toBe('данных пока нет');
  });
});

describe('groups and the flat list', () => {
  it('keeps the groups closed until asked and opens all of them under a filter', () => {
    const closed = groupsOf(admin, { kind: 'all', q: '' }, {}, NOW);
    expect(closed.length).toBeGreaterThan(0);
    expect(closed.every((g) => !g.open)).toBe(true);
    const first = closed[0]!;
    expect(groupsOf(admin, { kind: 'all', q: '' }, { [first.key]: true }, NOW)[0]!.open).toBe(true);
    expect(groupsOf(admin, { kind: 'skill', q: '' }, {}, NOW).every((g) => g.open)).toBe(true);
  });

  it('filters by kind and by the words of a row', () => {
    const skills = flatRows(admin, { kind: 'skill', q: '' }, NOW);
    expect(skills.length).toBeGreaterThan(0);
    expect(skills.every((r) => r.kind === 'skill')).toBe(true);
    const name = firstRow(admin).name;
    expect(flatRows(admin, { kind: 'all', q: name.toUpperCase() }, NOW).some((r) => r.name === name)).toBe(
      true,
    );
    expect(flatRows(admin, { kind: 'all', q: 'нет такого слова zzz' }, NOW)).toEqual([]);
  });

  it('finds a row with its layer', () => {
    const r = firstRow(admin);
    expect(findRow(admin, r.entryId)?.row.entryId).toBe(r.entryId);
    expect(findRow(admin, 'missing')).toBeNull();
  });
});

describe('the drawer draft', () => {
  it('reads the mode off the row: switched off wins over the audience', () => {
    expect(draftOf(row()).mode).toBe('all');
    expect(draftOf(row({ enabled: false })).mode).toBe('off');
    expect(draftOf(row({ audience: { mode: 'teams', teams: ['t1'] } }))).toEqual({
      mode: 'teams',
      teams: ['t1'],
      pinned: false,
    });
  });

  it('does not call a reordered team list a change', () => {
    const r = row({ audience: { mode: 'teams', teams: ['a', 'b'] } });
    const base = draftOf(r);
    expect(draftChanged(base, { ...base, teams: ['b', 'a'] })).toBe(false);
    expect(draftChanged(base, { ...base, teams: ['a'] })).toBe(true);
    expect(draftChanged(base, { ...base, pinned: true })).toBe(true);
  });

  it('wants at least one team for «only some teams»', () => {
    expect(draftValid({ mode: 'teams', teams: [], pinned: false })).toBe(false);
    expect(draftValid({ mode: 'teams', teams: ['a'], pinned: false })).toBe(true);
    expect(draftValid({ mode: 'off', teams: [], pinned: false })).toBe(true);
  });

  it('sends only the keys that changed', () => {
    const r = row();
    expect(draftPatch(r, draftOf(r), 'tenant')).toEqual({});
    expect(hasPatch(draftPatch(r, draftOf(r), 'tenant'))).toBe(false);
    expect(draftPatch(r, { mode: 'off', teams: [], pinned: false }, 'tenant')).toEqual({ enabled: false });
    expect(draftPatch(r, { mode: 'teams', teams: ['a'], pinned: true }, 'tenant')).toEqual({
      audience: { mode: 'teams', teams: ['a'] },
      pinned: true,
    });
  });

  it('sends no audience from a team layer, and none for a switched-off entry', () => {
    const r = row();
    expect(draftPatch(r, { mode: 'teams', teams: ['a'], pinned: false }, 'team')).toEqual({});
    const off = row({ enabled: false });
    expect(draftPatch(off, { mode: 'all', teams: [], pinned: false }, 'tenant')).toEqual({ enabled: true });
  });
});

describe('dayBars', () => {
  const bars = [
    { day: '2026-10-30', calls: 4 },
    { day: '2026-10-31', calls: 8 },
    { day: '2026-11-01', calls: 1 },
  ];

  it('scales to the busiest day and numbers every second bar', () => {
    const out = dayBars(bars, 13);
    expect(out.map((b) => b.h)).toEqual([50, 100, 13]);
    expect(out.map((b) => b.label)).toEqual(['30', '', '1']);
    expect(out[1]!.title).toBe('31.10 — 8 вызовов');
    expect(out[2]!.title).toBe('1.11 — 1 вызов');
  });

  it('draws a stub when the week had no calls', () => {
    expect(dayBars(bars, 0).every((b) => b.h === 3)).toBe(true);
    expect(dayBars(bars, null).every((b) => b.h === 3)).toBe(true);
  });
});

describe('optimistic changes of the view', () => {
  const r = firstRow(admin);

  it('applies a patch and recounts without touching the original', () => {
    const before = JSON.stringify(admin);
    const next = withPatch(admin, r.entryId, { enabled: !r.enabled });
    expect(findRow(next, r.entryId)?.row.enabled).toBe(!r.enabled);
    expect(next.summary.total).toBe(admin.summary.total + (r.enabled ? -1 : 1));
    expect(JSON.stringify(admin)).toBe(before);
  });

  it('puts the hub answer in place of the shown row', () => {
    const next = withRow(admin, { ...r, name: 'Другое имя' });
    expect(findRow(next, r.entryId)?.row.name).toBe('Другое имя');
  });

  it('removes an entry and puts it back where it was asked', () => {
    const found = findRow(admin, r.entryId)!;
    const gone = withoutEntry(admin, r.entryId);
    expect(findRow(gone, r.entryId)).toBeNull();
    const target = { layer: found.layer.layer, scopeId: found.layer.scopeId };
    const back = withAdded(gone, target, r);
    expect(findRow(back, r.entryId)?.row.entryId).toBe(r.entryId);
    expect(back.summary.total).toBe(admin.summary.total);
    expect(withAdded(back, target, r).layers.flatMap((l) => l.items).length).toBe(
      back.layers.flatMap((l) => l.items).length,
    );
  });

  it('finds what a catalog item already has in the target layer', () => {
    const found = findRow(admin, r.entryId)!;
    const target = { layer: found.layer.layer, scopeId: found.layer.scopeId };
    expect(entryIn(admin, target, r.itemId)?.entryId).toBe(r.entryId);
    expect(entryIn(admin, target, 'no-such-item')).toBeNull();
  });
});

describe('the catalog target', () => {
  const scope = { orgId: 'o1', tenantId: null as string | null };

  it('is the tenant for an admin looking at one, the organization for the whole', () => {
    expect(catalogTarget('admin', { ...scope, tenantId: 't1' }, null)).toEqual({
      layer: 'tenant',
      scopeId: 't1',
    });
    expect(catalogTarget('admin', scope, null)).toEqual({ layer: 'org', scopeId: 'o1' });
  });

  it('is the team of a lead, and nothing for a member or a lead without a team', () => {
    expect(catalogTarget('lead', scope, 'tm1')).toEqual({ layer: 'team', scopeId: 'tm1' });
    expect(catalogTarget('lead', scope, null)).toBeNull();
    expect(catalogTarget('member', scope, 'tm1')).toBeNull();
  });
});

describe('the member list', () => {
  const item = (over: Partial<MeEnvItem>): MeEnvItem => ({
    itemId: 'a',
    kind: 'skill',
    name: 'A',
    hint: '',
    layer: 'team',
    layerTitle: 'Команда Фронт',
    entryId: 'e',
    scopeId: 's',
    pinned: false,
    canToggle: true,
    off: false,
    ...over,
  });

  it('filters by kind, flips one item and names the source', () => {
    const items = [item({}), item({ itemId: 'b', kind: 'mcp' })];
    expect(mineFiltered(items, 'all')).toHaveLength(2);
    expect(mineFiltered(items, 'mcp').map((i) => i.itemId)).toEqual(['b']);
    expect(withMineOff(items, 'b', true).map((i) => i.off)).toEqual([false, true]);
    expect(items[1]!.off).toBe(false);
    expect(mineSource(item({ layer: 'org' }))).toBe('Организация');
    expect(mineSource(item({ layer: 'team' }))).toBe('Команда');
    expect(mineSource(item({ layer: 'tenant', layerTitle: 'Тенант Продукт' }))).toBe('Тенант Продукт');
  });
});

describe('delivery', () => {
  it('says how far the list got and what did not go in', () => {
    expect(deliveryTile({ machines: 0, reported: 0, current: 0, lastAt: null }, 3).value).toBe(
      'машин пока нет',
    );
    expect(deliveryTile({ machines: 4, reported: 4, current: 4, lastAt: null }, 3).value).toContain('4 из 4');
    expect(deliveryTile({ machines: 4, reported: 0, current: 0, lastAt: null }, 3).value).toContain(
      'ещё не отвечали',
    );
    expect(deliveryTile({ machines: 6, reported: 0, current: 6, lastAt: null }, 0).value).toBe(
      'доставлять пока нечего · 6 машин',
    );
    expect(deliveryIssue({ ok: 2, pending: 1, error: 0, message: null })).toContain(
      'не полностью на 1 машине',
    );
    expect(deliveryIssue({ ok: 1, pending: 0, error: 2, message: null })).toContain('не встало на 2 машинах');
    expect(deliveryIssue({ ok: 3, pending: 0, error: 0, message: null })).toBeNull();
  });
});
