// Fixtures for «Окружение»: the organization «Акме», its tenant «Продукт» and the board's three teams. State lives in
// this module: a switch or an audience saved here shows in the next read, the way the hub would answer. The rules follow
// docs/hub/environment.md: a lead edits their own team's layer only, a row pinned by the organization is locked for
// whoever looks at it from a tenant, an entry is in a layer once, and 404 `not_enabled` is the feature being off.
import type {
  AddCatalogRequest,
  AddEntryRequest,
  Audience,
  CatalogItem,
  CatalogKind,
  EnvCard,
  EnvEntryRow,
  EnvLayer,
  EnvLayerKind,
  EnvResponse,
  LockedReason,
  MeEnvItem,
  MeEnvResponse,
  ObservedTool,
  PatchEntryRequest,
} from '../../../hub-api/env';
import { OrgApiError } from '../api/errors';
import type { EnvApi } from '../api/env';
import { ALL_SCOPE } from '../types';
import { recount } from '../env-calc';
import { later, type MockCtx } from './ctx';
import { TEAMS, TENANTS } from './shell';

const MIN = 60_000;
const DAY = 24 * 60 * MIN;
const ORG_ID = 'org-acme';
const TENANT_ID = 't-product';

interface Item {
  id: string;
  kind: CatalogKind;
  name: string;
  hint: string;
  builtIn?: boolean;
}
interface Stored {
  entryId: string;
  itemId: string;
  layer: EnvLayerKind;
  scopeId: string;
  audience: Audience;
  pinned: boolean;
  enabled: boolean;
  calls: number;
  people: number;
  /** Minutes since the last call; null: never called. */
  ago: number | null;
  spark: number[];
  unused: boolean;
}

const all: Audience = { mode: 'tenant', teams: [] };
const some = (...teams: string[]): Audience => ({ mode: 'teams', teams });

// layer, scope, kind, name, hint, audience, pinned, enabled, calls, people, minutes ago, 14 days
type Seed = [
  EnvLayerKind,
  string,
  CatalogKind,
  string,
  string,
  Audience,
  boolean,
  boolean,
  number,
  number,
  number | null,
  number[],
  boolean?,
];
const Z = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0];
const DAYS = DAY / MIN;
const SEEDS: Seed[] = [
  [
    'org',
    ORG_ID,
    'mcp',
    'Трекер задач',
    'задачи и комментарии',
    all,
    true,
    true,
    520,
    28,
    0,
    [51, 40, 73, 35, 66, 19, 12, 64, 32, 59, 34, 36, 20, 30],
  ],
  [
    'org',
    ORG_ID,
    'skill',
    'Ревью кода',
    'проверка перед MR',
    all,
    true,
    true,
    690,
    28,
    0,
    [123, 95, 91, 45, 91, 15, 21, 89, 51, 77, 87, 90, 31, 35],
  ],
  [
    'org',
    ORG_ID,
    'skill',
    'Выкат в прод',
    'порядок и проверки',
    all,
    true,
    true,
    140,
    7,
    26 * 60,
    [18, 20, 15, 18, 9, 3, 4, 20, 16, 14, 19, 16, 5, 8],
  ],
  [
    'org',
    ORG_ID,
    'mcp',
    'GitLab',
    'MR, пайплайны, код',
    all,
    true,
    true,
    830,
    28,
    0,
    [131, 56, 79, 100, 84, 33, 39, 55, 102, 65, 84, 147, 32, 53],
  ],
  [
    'org',
    ORG_ID,
    'skill',
    'Секреты не в коде',
    'ловит ключи в диффе',
    all,
    true,
    true,
    210,
    11,
    26 * 60,
    [33, 27, 36, 20, 31, 10, 10, 24, 35, 38, 25, 30, 5, 11],
  ],
  [
    'org',
    ORG_ID,
    'doc',
    'Гайд по стилю кода',
    'агент читает перед правкой',
    all,
    false,
    true,
    0,
    0,
    34 * DAYS,
    Z,
    true,
  ],
  [
    'tenant',
    TENANT_ID,
    'repo',
    'platform/backend',
    'клонируется на машину',
    all,
    false,
    false,
    0,
    0,
    null,
    Z,
  ],
  [
    'tenant',
    TENANT_ID,
    'doc',
    'Архитектура продукта',
    'агент читает первым',
    all,
    false,
    true,
    380,
    21,
    60,
    [50, 55, 43, 57, 65, 14, 24, 39, 52, 46, 32, 36, 20, 14],
  ],
  [
    'tenant',
    TENANT_ID,
    'mcp',
    'Grafana',
    'метрики и логи',
    some('tm-front', 'tm-back'),
    false,
    true,
    260,
    14,
    5,
    [18, 30, 33, 44, 42, 15, 8, 29, 27, 44, 47, 20, 7, 8],
  ],
  [
    'tenant',
    TENANT_ID,
    'mcp',
    'Логи сервисов',
    'поиск по логам',
    all,
    false,
    true,
    170,
    9,
    26 * 60,
    [10, 28, 14, 16, 13, 7, 8, 17, 12, 28, 30, 24, 9, 7],
  ],
  [
    'tenant',
    TENANT_ID,
    'skill',
    'Тесты перед коммитом',
    'TDD и прогон',
    all,
    false,
    true,
    450,
    25,
    20,
    [72, 48, 49, 32, 62, 10, 10, 38, 35, 45, 29, 26, 12, 11],
  ],
  [
    'tenant',
    TENANT_ID,
    'doc',
    'Глоссарий продукта',
    'термины и сущности',
    all,
    false,
    true,
    0,
    0,
    34 * DAYS,
    Z,
    true,
  ],
  [
    'team',
    'tm-front',
    'repo',
    'product/web',
    'клонируется на машину',
    all,
    false,
    true,
    310,
    17,
    26 * 60,
    [42, 21, 26, 33, 43, 20, 15, 37, 22, 37, 57, 37, 11, 8],
  ],
  [
    'team',
    'tm-front',
    'skill',
    'Дизайн-система',
    'компоненты и токены',
    all,
    false,
    true,
    240,
    13,
    20,
    [24, 22, 39, 19, 14, 15, 11, 18, 30, 15, 30, 44, 14, 12],
  ],
  [
    'team',
    'tm-front',
    'mcp',
    'Браузер для проверки',
    'скриншоты и консоль',
    all,
    false,
    true,
    290,
    16,
    5,
    [36, 50, 30, 25, 37, 12, 14, 39, 46, 45, 24, 25, 11, 16],
  ],
  ['team', 'tm-front', 'skill', 'Storybook', 'истории компонентов', all, false, true, 0, 0, null, Z, true],
  [
    'team',
    'tm-back',
    'repo',
    'platform/api',
    'клонируется на машину',
    all,
    false,
    true,
    270,
    15,
    5,
    [33, 28, 16, 16, 25, 9, 14, 49, 31, 48, 50, 49, 10, 8],
  ],
  [
    'team',
    'tm-back',
    'mcp',
    'Postgres',
    'только чтение',
    all,
    false,
    true,
    160,
    8,
    5,
    [19, 16, 19, 29, 22, 3, 10, 16, 22, 26, 12, 17, 8, 5],
  ],
  [
    'team',
    'tm-back',
    'skill',
    'Миграции БД',
    'порядок и откат',
    all,
    false,
    true,
    60,
    3,
    3 * 60,
    [7, 8, 4, 11, 9, 2, 3, 4, 5, 11, 4, 8, 2, 3],
  ],
  [
    'team',
    'tm-design',
    'mcp',
    'Figma',
    'нужен вход каждого человека',
    all,
    false,
    true,
    120,
    6,
    5,
    [20, 22, 17, 12, 15, 3, 2, 22, 17, 15, 21, 14, 7, 7],
  ],
  [
    'team',
    'tm-design',
    'skill',
    'Иконки и иллюстрации',
    'экспорт и проверка',
    all,
    false,
    true,
    0,
    0,
    34 * DAYS,
    Z,
    true,
  ],
];

// Items that only the catalog has so far; the order is the board's, the rest follows the entries.
const EXTRA: Item[] = [
  { id: 'i-seo', kind: 'skill', name: 'SEO-аудит', hint: 'добавлен в 3 командах' },
  { id: 'i-mobile', kind: 'repo', name: 'product/mobile', hint: 'из подключения GitLab' },
];
const CATALOG_ORDER = [
  'SEO-аудит',
  'Figma',
  'Логи сервисов',
  'product/mobile',
  'Гайд по стилю кода',
  'Тесты перед коммитом',
];

// What the machines call that the catalog does not know yet: the list «Уже используют на машинах» starts with these.
const MACHINE_TOOLS: {
  kind: 'skill' | 'mcp';
  name: string;
  calls: number;
  week: number;
  people: number;
  ago: number;
}[] = [
  { kind: 'mcp', name: 'plugin_tools_sentry', calls: 41, week: 12, people: 3, ago: 90 },
  { kind: 'skill', name: 'tools:release', calls: 17, week: 6, people: 2, ago: 300 },
  { kind: 'mcp', name: 'claude_ai_Claude_Docs', calls: 5, week: 0, people: 1, ago: 9 * DAYS },
];

/** The catalog item a tool's calls are counted for, as the hub matches it: the name, a skill also by its bare name. */
const itemForTool = (kind: 'skill' | 'mcp', name: string): Item | undefined => {
  const bare = kind === 'skill' && name.includes(':') ? name.slice(name.lastIndexOf(':') + 1) : name;
  const list = [...items.values()].filter((i) => i.kind === kind);
  const low = (s: string) => s.toLowerCase();
  return list.find((i) => low(i.name) === low(name)) ?? list.find((i) => low(i.name) === low(bare));
};

const observedOf = (): ObservedTool[] => {
  const on = (itemId: string) => entries.some((e) => e.itemId === itemId && e.enabled);
  const fromEntries = new Map<string, ObservedTool>();
  for (const e of entries) {
    const it = items.get(e.itemId);
    if (!it || (it.kind !== 'skill' && it.kind !== 'mcp') || e.calls <= 0 || fromEntries.has(it.id)) continue;
    fromEntries.set(it.id, {
      kind: it.kind,
      name: it.name,
      itemId: it.id,
      inEnv: on(it.id),
      calls: e.calls * 4,
      callsWeek: e.calls,
      errors: 0,
      people: e.people,
      lastUsedAt: lastIso(e.ago),
    });
  }
  const loose = MACHINE_TOOLS.map((t): ObservedTool => {
    const it = itemForTool(t.kind, t.name);
    return {
      kind: t.kind,
      name: t.name,
      itemId: it?.id ?? null,
      inEnv: it ? on(it.id) : false,
      calls: t.calls,
      callsWeek: t.week,
      errors: 0,
      people: t.people,
      lastUsedAt: lastIso(t.ago),
    };
  });
  return [...fromEntries.values(), ...loose].sort((a, b) => b.calls - a.calls);
};

const items = new Map<string, Item>();
let entries: Stored[] = [];
// The member's own switches, by item id. The board starts with the repo of the tenant switched off.
const personalOff = new Set<string>();
// The entries a member gets from each layer in the mock (a short list, like the board's).
const MEMBER_ENTRIES = [0, 1, 2, 6, 7, 8, 12, 13, 14];
let serial = 100;

const seed = () => {
  items.clear();
  for (const x of EXTRA) items.set(x.id, x);
  entries = SEEDS.map((s, n): Stored => {
    const [layer, scopeId, kind, name, hint, audience, pinned, enabled, calls, people, ago, spark, unused] =
      s;
    const id = `i-e${n}`;
    items.set(id, { id, kind, name, hint, builtIn: n === 3 });
    return {
      entryId: `e${n}`,
      itemId: id,
      layer,
      scopeId,
      audience,
      pinned,
      enabled,
      calls,
      people,
      ago,
      spark,
      unused: !!unused,
    };
  });
  personalOff.clear();
  personalOff.add('i-e6');
};
seed();

/** For tests and the gallery: the mock back to the board's data. */
export const resetEnvMock = seed;

const orgTitle = 'Организация Акме';
const tenantName = (): string => TENANTS.find((t) => t.id === TENANT_ID)?.name ?? 'Продукт';
const teamName = (id: string): string => TEAMS.find((t) => t.id === id)?.name ?? id;

const layerInfo = (e: Stored): { title: string; subtitle: string } => {
  if (e.layer === 'org') return { title: orgTitle, subtitle: 'есть во всех тенантах' };
  if (e.layer === 'tenant') return { title: `Тенант ${tenantName()}`, subtitle: 'для всех команд тенанта' };
  const t = TEAMS.find((x) => x.id === e.scopeId);
  return {
    title: `Команда ${teamName(e.scopeId)}`,
    subtitle: `только людям команды${t?.leadName ? ` · лид ${t.leadName}` : ''}`,
  };
};

/** Why the viewer cannot change a layer's entry, or null. */
const lockOf = (ctx: MockCtx, e: Stored, scope: string): LockedReason | null => {
  if (ctx.role === 'admin')
    return e.layer === 'org' && e.pinned && scope !== ALL_SCOPE ? 'pinned_by_org' : null;
  if (e.layer === 'org') return e.pinned ? 'pinned_by_org' : 'no_rights';
  if (e.layer === 'tenant') return e.pinned ? 'pinned_by_tenant' : 'no_rights';
  return e.scopeId === 'tm-front' && ctx.role === 'lead' ? null : 'no_rights';
};

const lastIso = (ago: number | null): string | null =>
  ago === null ? null : new Date(Date.now() - ago * MIN).toISOString();

// Three machines hold the current list; a fourth has not answered. An MCP server with a secret waits for it on one.
const MACHINES = { machines: 4, reported: 3, current: 3 };
const deliveryOf = (e: Stored, kind: string): EnvEntryRow['delivery'] => {
  if (!e.enabled) return null;
  if (kind === 'mcp' && e.itemId === [...items.entries()].find(([, v]) => v.kind === 'mcp')?.[0])
    return {
      ok: 2,
      pending: 1,
      error: 0,
      message: 'written; the secret TRACKER_TOKEN is not on this machine, so the server starts without it',
    };
  return { ok: MACHINES.current, pending: 0, error: 0, message: null };
};

const rowOf = (ctx: MockCtx, e: Stored, scope: string): EnvEntryRow => {
  const it = items.get(e.itemId);
  const reason = lockOf(ctx, e, scope);
  return {
    entryId: e.entryId,
    itemId: e.itemId,
    kind: it?.kind ?? 'skill',
    name: it?.name ?? e.itemId,
    hint: it?.hint ?? '',
    audience: e.audience,
    pinned: e.pinned,
    locked: reason !== null,
    lockedReason: reason,
    enabled: e.enabled,
    callsWeek: e.calls,
    spark14: e.spark,
    lastUsedAt: lastIso(e.ago),
    unused: e.enabled && e.unused ? true : e.calls > 0 || !e.enabled ? false : null,
    delivery: deliveryOf(e, it?.kind ?? 'skill'),
  };
};

const view = (ctx: MockCtx, scope: string): EnvResponse => {
  const layers: EnvLayer[] = [];
  const order: { layer: EnvLayerKind; scopeId: string }[] = [
    { layer: 'org', scopeId: ORG_ID },
    { layer: 'tenant', scopeId: TENANT_ID },
    ...TEAMS.map((t) => ({ layer: 'team' as const, scopeId: t.id })),
  ];
  for (const o of order) {
    const rows = entries.filter((e) => e.layer === o.layer && e.scopeId === o.scopeId);
    const first = rows[0];
    const info = first ? layerInfo(first) : layerInfo({ ...(entries[0] as Stored), ...o });
    const mine = rows.map((e) => rowOf(ctx, e, scope));
    layers.push({
      layer: o.layer,
      scopeId: o.scopeId,
      tenantId: o.layer === 'org' ? '' : TENANT_ID,
      title: info.title,
      subtitle: info.subtitle,
      locked: mine.length > 0 ? mine.every((r) => r.locked) : ctx.role !== 'admin',
      callsWeek: rows.filter((e) => e.enabled).reduce((n, e) => n + e.calls, 0),
      counts: { skill: 0, mcp: 0, repo: 0, doc: 0 },
      items: mine,
    });
  }
  const top = entries
    .filter((e) => e.enabled && e.calls > 0)
    .sort((a, b) => b.calls - a.calls)
    .slice(0, 2)
    .map((e) => ({ id: e.itemId, name: items.get(e.itemId)?.name ?? e.itemId }));
  const base: EnvResponse = {
    summary: {
      total: 0,
      byKind: { skill: 0, mcp: 0, repo: 0, doc: 0 },
      callsWeek: layers.reduce((n, l) => n + (l.callsWeek ?? 0), 0),
      top,
      unused: entries.filter((e) => e.enabled && e.unused).length,
      pinnedByOrg: 0,
    },
    layers,
    teams: TEAMS.map((t) => ({ id: t.id, name: t.name, tenantId: TENANT_ID })),
    observed: observedOf(),
    delivery: { ...MACHINES, lastAt: lastIso(3) },
  };
  return recount(base);
};

const find = (entryId: string): Stored => {
  const e = entries.find((x) => x.entryId === entryId);
  if (!e) throw new OrgApiError('not_found', 'entry not found');
  return e;
};

const mayEdit = (ctx: MockCtx, e: Stored): void => {
  if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
  if (ctx.role === 'lead' && !(e.layer === 'team' && e.scopeId === 'tm-front')) {
    throw new OrgApiError(e.pinned ? 'locked' : 'forbidden', 'locked', 'tenant_admin');
  }
};

const bars = (spark: number[]): { day: string; calls: number }[] =>
  spark.map((calls, i) => ({
    day: new Date(Date.now() - (spark.length - 1 - i) * DAY).toISOString().slice(0, 10),
    calls,
  }));

const mineOf = (e: Stored): MeEnvItem => {
  const it = items.get(e.itemId);
  const off = personalOff.has(e.itemId);
  return {
    itemId: e.itemId,
    entryId: e.entryId,
    kind: it?.kind ?? 'skill',
    name: it?.name ?? e.itemId,
    hint: it?.hint ?? '',
    layer: e.layer,
    scopeId: e.scopeId,
    layerTitle: layerInfo(e).title,
    pinned: e.pinned,
    off,
    canToggle: !e.pinned,
  };
};

export const envMock = (ctx: MockCtx): EnvApi => ({
  envView: async (_org, scope) => {
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    return later(view(ctx, scope));
  },

  envCard: async (_org, entryId) => {
    const e = find(entryId);
    const it = items.get(e.itemId);
    const reason = lockOf(ctx, e, 'product');
    const card: EnvCard = {
      entryId: e.entryId,
      itemId: e.itemId,
      kind: it?.kind ?? 'skill',
      name: it?.name ?? e.itemId,
      hint: it?.hint ?? '',
      layer: e.layer,
      scopeId: e.scopeId,
      enabled: e.enabled,
      callsWeek: e.calls,
      people: e.people,
      perDay: Math.round(e.calls / 7),
      bars14: bars(e.spark),
      lastUsedAt: lastIso(e.ago),
      unused: e.enabled && e.unused ? true : e.calls > 0 || !e.enabled ? false : null,
      audience: e.audience,
      pinned: e.pinned,
      locked: reason !== null,
      lockedReason: reason,
    };
    return later(card, 220);
  },

  envAdd: async (_org, req: AddEntryRequest) => {
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    if (ctx.role === 'lead' && !(req.layer === 'team' && req.scopeId === 'tm-front')) {
      throw new OrgApiError('forbidden', 'forbidden', 'tenant_admin');
    }
    const it = items.get(req.itemId);
    if (!it) throw new OrgApiError('not_found', 'catalog item not found');
    if (entries.some((e) => e.itemId === req.itemId && e.layer === req.layer && e.scopeId === req.scopeId)) {
      throw new OrgApiError('exists', 'exists');
    }
    serial += 1;
    const created: Stored = {
      entryId: `e${serial}`,
      itemId: req.itemId,
      layer: req.layer,
      scopeId: req.scopeId,
      audience: req.audience ? { mode: req.audience.mode, teams: req.audience.teams ?? [] } : all,
      pinned: !!req.pinned,
      enabled: req.enabled ?? true,
      calls: 0,
      people: 0,
      ago: null,
      spark: Z,
      unused: false,
    };
    entries = [...entries, created];
    return later(rowOf(ctx, created, ALL_SCOPE), 500);
  },

  envPatch: async (_org, entryId, req: PatchEntryRequest) => {
    const e = find(entryId);
    mayEdit(ctx, e);
    if (req.pinned !== undefined && ctx.role !== 'admin')
      throw new OrgApiError('forbidden', 'forbidden', 'tenant_admin');
    if (req.enabled !== undefined) e.enabled = req.enabled;
    if (req.audience) {
      if (req.audience.mode === 'off') e.enabled = false;
      else
        e.audience = {
          mode: req.audience.mode,
          teams: req.audience.mode === 'teams' ? (req.audience.teams ?? []) : [],
        };
    }
    if (req.pinned !== undefined) e.pinned = req.pinned;
    return later(rowOf(ctx, e, ctx.role === 'admin' ? ALL_SCOPE : 'product'), 800);
  },

  envRemove: async (_org, entryId) => {
    const e = find(entryId);
    mayEdit(ctx, e);
    entries = entries.filter((x) => x.entryId !== entryId);
    return later(undefined, 500);
  },

  envCatalog: async (_org, target, kind) => {
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    const first = new Map(CATALOG_ORDER.map((n, i) => [n, i]));
    const list = [...items.values()]
      .filter((it) => !kind || it.kind === kind)
      .sort((a, b) => (first.get(a.name) ?? 99) - (first.get(b.name) ?? 99))
      .map((it): CatalogItem => ({
        id: it.id,
        kind: it.kind,
        name: it.name,
        hint: it.hint,
        source: {},
        builtIn: !!it.builtIn,
        archived: false,
        added: entries.some(
          (e) => e.itemId === it.id && e.layer === target.layer && e.scopeId === target.scopeId,
        ),
        createdAt: '2026-09-01T09:00:00Z',
      }));
    return later(list, 240);
  },

  catalogAdd: async (_org, req: AddCatalogRequest) => {
    if (ctx.role !== 'admin') throw new OrgApiError('forbidden', 'forbidden', 'org_admin');
    const name = req.name.trim();
    if ([...items.values()].some((i) => i.kind === req.kind && i.name.toLowerCase() === name.toLowerCase())) {
      throw new OrgApiError('name_taken', 'Такое название уже есть в каталоге');
    }
    serial += 1;
    const it: Item = { id: `i-n${serial}`, kind: req.kind, name, hint: req.hint ?? '' };
    items.set(it.id, it);
    return later(
      {
        id: it.id,
        kind: it.kind,
        name: it.name,
        hint: it.hint,
        source: req.source,
        builtIn: false,
        archived: false,
        added: false,
        createdAt: new Date().toISOString(),
      },
      300,
    );
  },

  envMine: async (): Promise<MeEnvResponse> => {
    const mine = MEMBER_ENTRIES.map((n) => entries.find((e) => e.entryId === `e${n}`)).filter(
      (e): e is Stored => !!e,
    );
    return later({
      space: { id: TENANT_ID, slug: 'product', name: tenantName() },
      org: { id: ORG_ID, name: 'Акме' },
      items: mine.map(mineOf),
    });
  },

  envMineSet: async (itemId, off) => {
    const e = entries.find((x) => x.itemId === itemId && MEMBER_ENTRIES.includes(Number(x.entryId.slice(1))));
    if (!e) throw new OrgApiError('not_found', 'item not found');
    if (e.pinned) throw new OrgApiError('locked', 'locked');
    if (off) personalOff.add(itemId);
    else personalOff.delete(itemId);
    return later(mineOf(e), 600);
  },
});
