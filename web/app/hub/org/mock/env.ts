// The mock hub of «Окружение»: the organization «Акме», its tenant «Продукт» and three teams, with the items of the board.
// State lives in this module, so a change shows in the next read, the way the hub answers. The rules follow
// docs/hub/environment.md: an admin changes every layer, a lead their own team's, an item that belongs to a layer goes
// into that layer only, a script that calls brew is refused, the order of scripts is the order of the layer's entries.
// A sandbox run goes step by step: it can fail (no pnpm yet, brew) and pass after the order is mended.
import type {
  AddCatalogRequest,
  AddEntryRequest,
  AddGroupRequest,
  AddPresetRequest,
  ApplyRequest,
  CatalogItem,
  CatalogKind,
  CatalogOwner,
  CatalogSource,
  EnvCard,
  EnvEntryRow,
  EnvGroup,
  EnvLayer,
  EnvLayerKind,
  EnvPreset,
  EnvResponse,
  EnvRunRow,
  EnvRunStep,
  LockedReason,
  MeEnvGroup,
  MeEnvItem,
  MeEnvPreset,
  MeEnvResponse,
  MeMachine,
  PatchCatalogRequest,
  PatchEntryRequest,
} from '../../../hub-api/env';
import { OrgApiError } from '../api/errors';
import type { EnvApi } from '../api/env';
import { recount } from '../env-calc';
import type { Role } from '../types';
import { later, type MockCtx } from './ctx';
import { TEAMS, TENANTS, VIEWERS } from './shell';

const MIN = 60_000;
const ORG_ID = 'org-acme';
const TENANT_ID = 't-product';
const MY_TEAM = 'tm-front';

interface Item {
  id: string;
  kind: CatalogKind;
  name: string;
  hint: string;
  source: CatalogSource;
  owner: CatalogOwner | null;
  archived: boolean;
}
interface Entry {
  entryId: string;
  itemId: string;
  layer: EnvLayerKind;
  scopeId: string;
  pinned: boolean;
  enabled: boolean;
  groupId?: string;
}

interface Group extends EnvGroup {
  layer: EnvLayerKind;
  scopeId: string;
}

/** The platform's own group, as the hub puts it into every organization. */
const RECOMMENDED = 'recommended';

const team = (id: string): CatalogOwner => ({ layer: 'team', scopeId: id });
const tenant: CatalogOwner = { layer: 'tenant', scopeId: TENANT_ID };

const SEED_ITEMS: Item[] = [
  {
    id: 'builtin-superpowers',
    kind: 'plugin',
    name: 'Superpowers',
    hint: 'процесс работы: план, тесты, проверка перед сдачей',
    source: {
      marketplace: 'anthropics/claude-plugins-official',
      plugin: 'superpowers@claude-plugins-official',
    },
    owner: null,
  },
  {
    id: 'builtin-claude-settings',
    kind: 'claude-settings',
    name: 'Экономные настройки Claude',
    hint: 'сабагенты на Sonnet, разумные таймауты, правила экономии в CLAUDE.md',
    source: {},
    owner: null,
  },
  {
    id: 'i-gitlab',
    kind: 'access',
    name: 'GitLab',
    hint: 'glab и git · каждый входит своим аккаунтом',
    source: { service: 'gitlab', host: 'gitlab.example.ru' },
    owner: null,
  },
  {
    id: 'i-yt',
    kind: 'mcp',
    name: 'YouTrack',
    hint: 'задачи из трекера · вход своим аккаунтом',
    source: { transport: 'http', url: 'https://tracker.example.ru/mcp' },
    owner: null,
  },
  {
    id: 'i-howwork',
    kind: 'skill',
    name: 'Как мы работаем',
    hint: 'задачи, ветки, ревью · обновляется сам',
    source: { ref: 'https://gitlab.example.ru/org/skills/how-we-work' },
    owner: null,
  },
  {
    id: 'i-review',
    kind: 'skill',
    name: 'Код-ревью',
    hint: 'чек-лист ревью MR · обновляется сам',
    source: { ref: 'https://gitlab.example.ru/org/skills/code-review' },
    owner: null,
  },
  {
    id: 'i-sentry',
    kind: 'mcp',
    name: 'Sentry',
    hint: 'ошибки продакшена · вход своим аккаунтом',
    source: { transport: 'http', url: 'https://mcp.sentry.dev/mcp' },
    owner: tenant,
  },
  {
    id: 'i-ds',
    kind: 'repo',
    name: 'design-system',
    hint: 'frontend/design-system · ветка main',
    source: { url: 'https://gitlab.example.ru/frontend/design-system', branch: 'main' },
    owner: tenant,
  },
  {
    id: 'i-copy',
    kind: 'skill',
    name: 'Тексты интерфейса',
    hint: 'тон, «мероприятие», без канцелярита · написан здесь',
    source: {
      body: '---\nname: ui-copy\ndescription: Как писать тексты интерфейса\n---\n\nПишем «мероприятие», а не «событие».',
    },
    owner: tenant,
  },
  {
    id: 'i-web',
    kind: 'repo',
    name: 'web',
    hint: 'frontend/web · ветка main',
    source: { url: 'https://gitlab.example.ru/frontend/web', branch: 'main' },
    owner: team('tm-front'),
  },
  {
    id: 'i-adminui',
    kind: 'repo',
    name: 'admin-ui',
    hint: 'frontend/admin-ui · ветка main',
    source: { url: 'https://gitlab.example.ru/frontend/admin-ui', branch: 'main' },
    owner: team('tm-front'),
  },
  {
    id: 'i-setup',
    kind: 'script',
    name: 'Настройка web',
    hint: 'scripts/setup-dev.sh из web · после клона',
    source: {
      from: 'repo',
      repo: 'i-web',
      path: 'scripts/setup-dev.sh',
      rerun: 'check',
      check: 'test -f ~/code/web/.env.local',
      secrets: ['npm-token'],
    },
    owner: team('tm-front'),
  },
  {
    id: 'i-node',
    kind: 'script',
    name: 'Node и pnpm',
    hint: '3 команды · при настройке машины',
    source: {
      from: 'text',
      body: 'curl -fsSL https://fnm.vercel.app/install | bash\nfnm install 22\ncorepack enable pnpm',
      rerun: 'check',
      check: 'node -v | grep -q v22',
    },
    owner: team('tm-front'),
  },
  {
    id: 'i-hooks',
    kind: 'script',
    name: 'Git-хуки',
    hint: 'scripts/install-hooks.sh из web · после клона',
    source: { from: 'repo', repo: 'i-web', path: 'scripts/install-hooks.sh', rerun: 'once' },
    owner: team('tm-front'),
  },
  {
    id: 'i-changelog',
    kind: 'skill',
    name: 'Changelog',
    hint: 'пишет changelog к релизу · обновляется сам',
    source: { ref: 'https://gitlab.example.ru/frontend/skills/changelog' },
    owner: team('tm-front'),
  },
  {
    id: 'i-figma',
    kind: 'mcp',
    name: 'Figma',
    hint: 'макеты · вход своим аккаунтом',
    source: { transport: 'http', url: 'https://mcp.figma.com/mcp' },
    owner: null,
  },
  {
    id: 'i-github',
    kind: 'access',
    name: 'GitHub',
    hint: 'gh и git · каждый входит своим аккаунтом',
    source: { service: 'github', host: 'github.com' },
    owner: team('tm-back'),
  },
  {
    id: 'i-api',
    kind: 'repo',
    name: 'api',
    hint: 'backend/api · ветка main',
    source: { url: 'https://gitlab.example.ru/backend/api', branch: 'main' },
    owner: team('tm-back'),
  },
  {
    id: 'i-gostyle',
    kind: 'skill',
    name: 'Стиль Go',
    hint: 'как пишем сервисы · обновляется сам',
    source: { ref: 'https://gitlab.example.ru/backend/skills/go-style' },
    owner: team('tm-back'),
  },
  {
    id: 'i-pg',
    kind: 'mcp',
    name: 'Postgres',
    hint: 'чтение тестовых баз · общий ключ POSTGRES_TEST',
    source: {
      transport: 'stdio',
      command: 'npx',
      args: ['@modelcontextprotocol/server-postgres'],
      secretRefs: ['POSTGRES_TEST'],
    },
    owner: team('tm-back'),
  },
  {
    id: 'i-sinopia',
    kind: 'skill',
    name: 'Sinopia',
    hint: 'доски и макеты · обновляется сам',
    source: { ref: 'https://gitlab.example.ru/design/skills/sinopia' },
    owner: team('tm-design'),
  },
].map((i) => ({ ...i, archived: false }) as Item);

// layer, scope, item, pinned
const SEED_ENTRIES: [EnvLayerKind, string, string, boolean][] = [
  ['org', ORG_ID, 'i-gitlab', true],
  ['org', ORG_ID, 'i-yt', true],
  ['org', ORG_ID, 'i-howwork', true],
  ['org', ORG_ID, 'i-review', true],
  ['tenant', TENANT_ID, 'i-sentry', true],
  ['tenant', TENANT_ID, 'i-ds', true],
  ['tenant', TENANT_ID, 'i-copy', true],
  ['team', 'tm-front', 'i-web', true],
  ['team', 'tm-front', 'i-adminui', true],
  ['team', 'tm-front', 'i-setup', true],
  ['team', 'tm-front', 'i-node', true],
  ['team', 'tm-front', 'i-hooks', true],
  ['team', 'tm-front', 'i-changelog', false],
  ['team', 'tm-front', 'i-figma', false],
  ['team', 'tm-back', 'i-github', true],
  ['team', 'tm-back', 'i-api', true],
  ['team', 'tm-back', 'i-gostyle', false],
  ['team', 'tm-back', 'i-pg', false],
  ['team', 'tm-design', 'i-figma', false],
  ['team', 'tm-design', 'i-sinopia', true],
];

const items = new Map<string, Item>();
let entries: Entry[] = [];
let groups: Group[] = [];
interface Preset extends EnvPreset {
  layer: EnvLayerKind;
  scopeId: string;
}
let presets: Preset[] = [];
/** The member's own choices of optional groups. */
const groupOn = new Map<string, boolean>();
let runs: EnvRunRow[] = [];
const logs = new Map<string, string>();
let serial = 100;
const timers: number[] = [];
let machineCurrent = false;
let machineFailed = 1;

const iso = (agoMin: number): string => new Date(Date.now() - agoMin * MIN).toISOString();

const seed = () => {
  for (const t of timers) window.clearTimeout(t);
  timers.length = 0;
  items.clear();
  for (const it of SEED_ITEMS) items.set(it.id, it);
  entries = SEED_ENTRIES.map(([layer, scopeId, itemId, pinned], n) => ({
    entryId: `e${n}`,
    itemId,
    layer,
    scopeId,
    pinned,
    enabled: true,
  }));
  groups = [
    {
      id: 'g-back',
      layer: 'org',
      scopeId: ORG_ID,
      name: 'Бэкенд',
      hint: 'Сервисы и их базы',
      required: false,
      defaultOn: false,
      position: 0,
      builtIn: false,
    },
    {
      id: RECOMMENDED,
      layer: 'org',
      scopeId: ORG_ID,
      name: 'Рекомендованное',
      hint: 'плагины и настройки, с которыми агенту лучше; включает каждый у себя',
      required: false,
      defaultOn: false,
      position: 1 << 20,
      builtIn: true,
    },
  ];
  presets = [
    {
      id: 'p-dev',
      layer: 'org',
      scopeId: ORG_ID,
      name: 'Разработчик',
      hint: 'сервисы и рекомендованное',
      groupIds: ['g-back', RECOMMENDED],
      position: 0,
    },
    {
      id: 'p-back',
      layer: 'org',
      scopeId: ORG_ID,
      name: 'Только бэкенд',
      hint: '',
      groupIds: ['g-back'],
      position: 1,
    },
  ];
  groupOn.clear();
  // One item of the organization sits in the optional group, so both views show a group.
  const inGroup =
    entries.find((e) => e.layer === 'org' && !e.pinned) ?? entries.find((e) => e.layer === 'org');
  if (inGroup) inGroup.groupId = 'g-back';
  for (const id of ['builtin-superpowers', 'builtin-claude-settings']) {
    entries.push({
      entryId: `builtin-entry-${id}`,
      itemId: id,
      layer: 'org',
      scopeId: ORG_ID,
      pinned: false,
      enabled: true,
      groupId: RECOMMENDED,
    });
  }
  machineCurrent = false;
  machineFailed = 1;
  logs.clear();
  const mk = (id: string, person: string, machine: string, ago: number, steps: EnvRunStep[]): EnvRunRow => ({
    id,
    machine: { id: `m-${machine}`, name: machine },
    person: { id: `u-${person}`, name: person },
    mode: 'apply',
    by: null,
    status: steps.some((s) => s.status === 'error') ? 'error' : 'ok',
    startedAt: iso(ago),
    finishedAt: iso(ago - 1),
    steps,
  });
  const st = (
    itemId: string,
    kind: CatalogKind,
    name: string,
    status: EnvRunStep['status'],
    ms: number,
    message: string | null = null,
  ): EnvRunStep => ({
    itemId,
    kind,
    name,
    status,
    message,
    exit: status === 'error' ? 127 : status === 'ok' ? 0 : null,
    ms,
  });
  runs = [
    mk('run-a1', 'Анна', 'vm-anna', 20, [
      st('i-web', 'repo', 'web', 'ok', 9000),
      st('i-node', 'script', 'Node и pnpm', 'ok', 6400),
      st('i-setup', 'script', 'Настройка web', 'error', 3000, 'pnpm: command not found'),
    ]),
    mk('run-b1', 'Борис', 'vm-boris', 90, [
      st('i-web', 'repo', 'web', 'ok', 8000),
      st('i-setup', 'script', 'Настройка web', 'error', 3000, 'pnpm: command not found'),
    ]),
    mk('run-o1', 'Олег', 'vm-oleg', 300, [
      st('i-web', 'repo', 'web', 'ok', 8800),
      st('i-figma', 'mcp', 'Figma', 'ok', 2000),
      st('i-changelog', 'skill', 'Changelog', 'ok', 1000),
    ]),
  ];
  logs.set(
    'run-a1:i-setup',
    'HOME=/home/anna NPM_TOKEN=••••••\n$ cd ~/code/web && bash scripts/setup-dev.sh\n+ pnpm install --frozen-lockfile\nscripts/setup-dev.sh: line 4: pnpm: command not found\nexit 127',
  );
  logs.set(
    'run-b1:i-setup',
    'HOME=/home/boris NPM_TOKEN=••••••\n$ cd ~/code/web && bash scripts/setup-dev.sh\n+ pnpm install --frozen-lockfile\nscripts/setup-dev.sh: line 4: pnpm: command not found\nexit 127',
  );
};
seed();

/** For tests and the gallery: the mock back to the board's data. */
export const resetEnvMock = seed;

// ── layers ────────────────────────────────────────────────────────────────────────────────────────────────────────

const tenantName = (): string => TENANTS.find((t) => t.id === TENANT_ID)?.name ?? 'Продукт';
const teamName = (id: string): string => TEAMS.find((t) => t.id === id)?.name ?? id;

const layerTitle = (layer: EnvLayerKind, scopeId: string): string => {
  if (layer === 'org') return 'Организация Акме';
  if (layer === 'tenant') return `Тенант ${tenantName()}`;
  return `Команда ${teamName(scopeId)}`;
};

const mayManage = (role: Role, layer: EnvLayerKind, scopeId: string): boolean => {
  if (role === 'admin') return true;
  return role === 'lead' && layer === 'team' && scopeId === MY_TEAM;
};

const guard = (role: Role, layer: EnvLayerKind, scopeId: string): void => {
  if (!mayManage(role, layer, scopeId))
    throw new OrgApiError('forbidden', 'forbidden', layer === 'team' ? 'team_lead' : 'org_admin');
};

const rowOf = (role: Role, e: Entry): EnvEntryRow => {
  const it = items.get(e.itemId);
  const can = mayManage(role, e.layer, e.scopeId);
  let reason: LockedReason | null = can ? null : e.layer === 'org' ? 'pinned_by_org' : 'no_rights';
  if (e.entryId.startsWith('builtin-entry-')) reason = 'built_in';
  return {
    entryId: e.entryId,
    itemId: e.itemId,
    kind: it?.kind ?? 'skill',
    name: it?.name ?? e.itemId,
    hint: it?.hint ?? '',
    audience: { mode: 'tenant', teams: [] },
    pinned: e.pinned,
    locked: reason !== null,
    lockedReason: reason,
    enabled: e.enabled,
    callsWeek: null,
    spark14: Array.from({ length: 14 }, () => 0),
    lastUsedAt: null,
    unused: null,
    delivery: null,
    groupId: e.groupId ?? '',
  };
};

const groupView = ({ layer: _l, scopeId: _s, ...g }: Group): EnvGroup => g;
const isOn = (g: Group): boolean => g.required || (groupOn.get(g.id) ?? g.defaultOn);
const findGroup = (id: string): Group => {
  const g = groups.find((x) => x.id === id);
  if (!g) throw new OrgApiError('not_found', 'group not found');
  return g;
};

const presetView = ({ layer: _l, scopeId: _s, ...p }: Preset): EnvPreset => p;

/** The groups the person's items are in, and the presets they can pick, as the hub lists them. */
const mineGroups = (mine: Entry[]): MeEnvGroup[] =>
  groups
    .filter((g) => mine.some((e) => e.groupId === g.id))
    .map((g) => ({
      id: g.id,
      name: g.name,
      hint: g.hint,
      layer: g.layer,
      required: g.required,
      on: isOn(g),
      canToggle: !g.required,
      items: mine.filter((e) => e.groupId === g.id).length,
      builtIn: g.builtIn,
    }));

const minePresets = (mine: MeEnvGroup[]): MeEnvPreset[] =>
  presets
    .filter((p) => p.groupIds.some((id) => mine.some((g) => g.id === id)))
    .map((p) => ({
      id: p.id,
      name: p.name,
      hint: p.hint,
      layer: p.layer,
      groupIds: p.groupIds,
      active: mine
        .filter((g) => !g.required && g.layer === p.layer)
        .every((g) => g.on === p.groupIds.includes(g.id)),
    }));

const findPreset = (id: string): Preset => {
  const p = presets.find((x) => x.id === id);
  if (!p) throw new OrgApiError('not_found', 'preset not found');
  return p;
};

/** Every group of a preset is a group of its own layer. */
const checkPresetGroups = (layer: EnvLayerKind, scopeId: string, ids: string[]): void => {
  if (ids.some((id) => !groups.some((g) => g.id === id && g.layer === layer && g.scopeId === scopeId)))
    throw new OrgApiError('invalid_request', 'Группа должна быть из этого же слоя');
};

const view = (ctx: MockCtx): EnvResponse => {
  const order: { layer: EnvLayerKind; scopeId: string }[] = [
    { layer: 'org', scopeId: ORG_ID },
    { layer: 'tenant', scopeId: TENANT_ID },
    ...TEAMS.map((t) => ({ layer: 'team' as const, scopeId: t.id })),
  ];
  const layers: EnvLayer[] = order
    .filter((o) => ctx.role === 'admin' || o.layer !== 'team' || o.scopeId === MY_TEAM || ctx.role === 'lead')
    .map((o) => ({
      layer: o.layer,
      scopeId: o.scopeId,
      tenantId: o.layer === 'org' ? '' : TENANT_ID,
      title: layerTitle(o.layer, o.scopeId),
      subtitle: '',
      locked: !mayManage(ctx.role, o.layer, o.scopeId),
      callsWeek: null,
      counts: { skill: 0, mcp: 0, repo: 0, doc: 0 },
      items: entries
        .filter((e) => e.layer === o.layer && e.scopeId === o.scopeId)
        .map((e) => rowOf(ctx.role, e)),
      groups: groups.filter((g) => g.layer === o.layer && g.scopeId === o.scopeId).map(groupView),
      presets: presets.filter((p) => p.layer === o.layer && p.scopeId === o.scopeId).map(presetView),
    }));
  const base: EnvResponse = {
    summary: {
      total: 0,
      byKind: { skill: 0, mcp: 0, repo: 0, doc: 0 },
      callsWeek: null,
      top: [],
      unused: null,
      pinnedByOrg: 0,
    },
    layers,
    teams: TEAMS.map((t) => ({ id: t.id, name: t.name, tenantId: TENANT_ID })),
    observed: [],
    delivery: { machines: 4, reported: 3, current: 3, lastAt: iso(3) },
  };
  return recount(base);
};

const findEntry = (entryId: string): Entry => {
  const e = entries.find((x) => x.entryId === entryId);
  if (!e) throw new OrgApiError('not_found', 'entry not found');
  return e;
};
const findItem = (id: string): Item => {
  const it = items.get(id);
  if (!it) throw new OrgApiError('not_found', 'catalog item not found');
  return it;
};

const BREW = /(^|[\s;&|])brew\s/m;
const refuseBrew = (kind: CatalogKind, source: CatalogSource | undefined): void => {
  if (kind !== 'script' || !source) return;
  if (BREW.test(source.body ?? ''))
    throw new OrgApiError('invalid_request', 'brew на Linux-машинах нет: поставьте через apt или curl');
  if (BREW.test(source.run ?? ''))
    throw new OrgApiError('invalid_request', 'brew на Linux-машинах нет: поставьте через apt или curl');
};

const SECRETS_ONLY_ADMIN = 'Доступы и токены из сейфа подключает админ организации';
const refuseSecrets = (role: Role, kind: CatalogKind, source: CatalogSource | undefined): void => {
  if (role === 'admin') return;
  if (kind === 'access' || (kind === 'script' && (source?.secrets?.length ?? 0) > 0))
    throw new OrgApiError('invalid_request', SECRETS_ONLY_ADMIN);
};

const catalogOf = (it: Item, layer?: EnvLayerKind, scopeId?: string): CatalogItem => ({
  id: it.id,
  kind: it.kind,
  name: it.name,
  hint: it.hint,
  source: it.source,
  builtIn: false,
  archived: it.archived,
  added: entries.some((e) => e.itemId === it.id && e.layer === layer && e.scopeId === scopeId),
  createdAt: '2026-10-20T09:00:00Z',
  owner: it.owner,
});

// ── runs ──────────────────────────────────────────────────────────────────────────────────────────────────────────

const KIND_RANK: Record<string, number> = { access: 0, repo: 1, script: 2, skill: 3, mcp: 4, doc: 5 };
const LAYER_RANK: Record<EnvLayerKind, number> = { org: 0, tenant: 1, team: 2 };

/** The list a layer's people get, in the order the machine does it: access, repositories, scripts (top layer first), the rest. */
const listFor = (layer: EnvLayerKind, scopeId: string): { itemId: string; layer: EnvLayerKind }[] => {
  const teamId = layer === 'team' ? scopeId : null;
  const ids = entries.filter(
    (e) =>
      e.enabled &&
      (e.layer === 'org' ||
        (e.layer === 'tenant' && layer !== 'org') ||
        (e.layer === 'team' && e.scopeId === teamId)),
  );
  const own = ids.filter((e) =>
    layer === 'org' ? e.layer === 'org' : layer === 'tenant' ? e.layer !== 'team' : true,
  );
  return [...own]
    .map((e, i) => ({ e, i }))
    .sort((a, b) => {
      const ka = KIND_RANK[items.get(a.e.itemId)?.kind ?? 'doc'] ?? 9;
      const kb = KIND_RANK[items.get(b.e.itemId)?.kind ?? 'doc'] ?? 9;
      return ka - kb || LAYER_RANK[a.e.layer] - LAYER_RANK[b.e.layer] || a.i - b.i;
    })
    .map(({ e }) => ({ itemId: e.itemId, layer: e.layer }));
};

const textOf = (it: Pick<Item, 'source' | 'name'>): string =>
  [it.source.body, it.source.run, it.source.path, it.source.check].filter(Boolean).join('\n');

interface Plan {
  item: Pick<Item, 'id' | 'kind' | 'name' | 'source'>;
}

/** Whether a step falls and the words it falls with. */
const outcome = (plan: Plan[], i: number): { exit: number; log: string; message: string } | null => {
  const it = plan[i]?.item;
  if (!it || it.kind !== 'script') return null;
  const text = textOf(it);
  const cmd = BREW.test(text) ? 'brew' : /setup-dev/.test(text) || /pnpm/.test(text) ? 'pnpm' : null;
  if (!cmd) return null;
  if (cmd === 'pnpm') {
    const before = plan
      .slice(0, i)
      .some((p) => p.item.kind === 'script' && /pnpm/.test(textOf(p.item)) && p.item.source.from === 'text');
    if (before || /corepack enable pnpm/.test(text)) return null;
  }
  const secrets = (it.source.secrets ?? [])
    .map((s) => `${s.toUpperCase().replace(/[^A-Z0-9]/g, '_')}=••••••`)
    .join(' ');
  const run =
    it.source.run || (it.source.path ? `bash ${it.source.path}` : (it.source.body?.split('\n')[0] ?? ''));
  return {
    exit: 127,
    message: `${cmd}: command not found`,
    log: `${secrets ? `HOME=/home/andrey ${secrets}\n` : ''}$ ${run}\n${cmd === 'pnpm' ? '+ pnpm install --frozen-lockfile\n' : ''}bash: ${cmd}: command not found\nexit 127`,
  };
};

const stepDelay = 700;

const startSandbox = (
  ctx: MockCtx,
  layer: EnvLayerKind,
  scopeId: string,
  draft: { entryId?: string; kind: CatalogKind; name: string; source: CatalogSource } | undefined,
  machine: { id: string; name: string },
): string => {
  serial += 1;
  const id = `run-s${serial}`;
  let list = listFor(layer, scopeId).map((x) => {
    const it = findItem(x.itemId);
    return { item: { id: it.id, kind: it.kind, name: it.name, source: it.source } } satisfies Plan;
  });
  if (draft) {
    const replaced = draft.entryId ? entries.find((e) => e.entryId === draft.entryId)?.itemId : undefined;
    const d: Plan = {
      item: { id: replaced ?? 'draft', kind: draft.kind, name: draft.name, source: draft.source },
    };
    if (replaced && list.some((p) => p.item.id === replaced))
      list = list.map((p) => (p.item.id === replaced ? d : p));
    else list = [...list, d];
  }
  const person = VIEWERS[ctx.role];
  const run: EnvRunRow = {
    id,
    machine,
    person: { id: person.id, name: person.name },
    mode: 'sandbox',
    by: person.name,
    status: 'running',
    startedAt: new Date().toISOString(),
    finishedAt: null,
    steps: list.map((p) => ({
      itemId: p.item.id,
      kind: p.item.kind,
      name: p.item.name,
      status: 'pending',
      message: null,
      exit: null,
      ms: 0,
    })),
  };
  runs = [run, ...runs];
  let failed: string | null = null;
  const step = (i: number) => {
    const s = run.steps[i];
    if (!s) {
      run.status = run.steps.some((x) => x.status === 'error') ? 'error' : 'ok';
      run.finishedAt = new Date().toISOString();
      return;
    }
    if (s.kind === 'script' && failed) {
      s.status = 'pending';
      s.message = `ждёт, пока починят «${failed}»`;
      timers.push(window.setTimeout(() => step(i + 1), 80));
      return;
    }
    s.status = 'running';
    timers.push(
      window.setTimeout(() => {
        const bad = outcome(list, i);
        s.ms = 1500 + ((i * 1700) % 6000);
        if (bad) {
          s.status = 'error';
          s.exit = bad.exit;
          s.message = bad.message;
          logs.set(`${id}:${s.itemId}`, bad.log);
          if (s.kind === 'script') failed = s.name;
        } else {
          s.status = 'ok';
          s.exit = 0;
          logs.set(`${id}:${s.itemId}`, `$ ${s.kind === 'repo' ? 'git clone' : 'готово'}\nexit 0`);
        }
        step(i + 1);
      }, stepDelay),
    );
  };
  timers.push(window.setTimeout(() => step(0), 350));
  return id;
};

const withLogs = (r: EnvRunRow): EnvRunRow => ({
  ...r,
  steps: r.steps.map((s) => {
    const log = logs.get(`${r.id}:${s.itemId}`);
    return { ...s, ...(log ? { log } : {}) };
  }),
});
const bare = (r: EnvRunRow): EnvRunRow => ({ ...r, steps: r.steps.map((s) => ({ ...s })) });

const MY_MACHINE = { id: 'm-andrey', name: 'vm-andrey' };
const myMachine = (): MeMachine => ({
  ...MY_MACHINE,
  online: true,
  team: { id: MY_TEAM, name: teamName(MY_TEAM) },
  setup: true,
  current: machineCurrent,
  failed: machineFailed,
  lastRun: runs.find((r) => r.machine.id === MY_MACHINE.id && r.mode !== 'sandbox') ?? null,
});

const mineOf = (e: Entry): MeEnvItem => {
  const it = findItem(e.itemId);
  const g = groups.find((x) => x.id === e.groupId);
  return {
    itemId: e.itemId,
    entryId: e.entryId,
    kind: it.kind,
    name: it.name,
    hint: it.hint,
    layer: e.layer,
    scopeId: e.scopeId,
    layerTitle: layerTitle(e.layer, e.scopeId),
    pinned: e.pinned || (g?.required ?? false),
    off: g ? !isOn(g) : false,
    canToggle: !e.pinned && !g,
    groupId: e.groupId ?? '',
  };
};

const startMine = (mode: 'update' | 'check'): string => {
  serial += 1;
  const id = `run-m${serial}`;
  const list = listFor('team', MY_TEAM);
  const run: EnvRunRow = {
    id,
    machine: MY_MACHINE,
    person: { id: VIEWERS.member.id, name: VIEWERS.member.name },
    mode,
    by: null,
    status: 'running',
    startedAt: new Date().toISOString(),
    finishedAt: null,
    steps: list.map((x) => {
      const it = findItem(x.itemId);
      return {
        itemId: it.id,
        kind: it.kind,
        name: it.name,
        status: 'pending',
        message: null,
        exit: null,
        ms: 0,
      };
    }),
  };
  runs = [run, ...runs];
  const step = (i: number) => {
    const s = run.steps[i];
    if (!s) {
      run.status = 'ok';
      run.finishedAt = new Date().toISOString();
      if (mode === 'update') {
        machineCurrent = true;
        machineFailed = 0;
      }
      return;
    }
    s.status = 'running';
    timers.push(
      window.setTimeout(() => {
        s.status = 'ok';
        s.exit = 0;
        s.ms = 1200;
        step(i + 1);
      }, 300),
    );
  };
  timers.push(window.setTimeout(() => step(0), 300));
  return id;
};

// ── the api ───────────────────────────────────────────────────────────────────────────────────────────────────────

export const envMock = (ctx: MockCtx): EnvApi => ({
  envView: async () => {
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    return later(view(ctx));
  },

  envCard: async (_org, entryId) => {
    const e = findEntry(entryId);
    const r = rowOf(ctx.role, e);
    const card: EnvCard = {
      entryId: e.entryId,
      itemId: e.itemId,
      kind: r.kind,
      name: r.name,
      hint: r.hint,
      layer: e.layer,
      scopeId: e.scopeId,
      enabled: e.enabled,
      callsWeek: null,
      people: null,
      perDay: null,
      bars14: [],
      lastUsedAt: null,
      unused: null,
      audience: r.audience,
      pinned: e.pinned,
      locked: r.locked,
      lockedReason: r.lockedReason,
    };
    return later(card, 220);
  },

  envAdd: async (_org, req: AddEntryRequest) => {
    guard(ctx.role, req.layer, req.scopeId);
    const it = findItem(req.itemId);
    if (it.archived) throw new OrgApiError('invalid_request', 'Этот пункт убран из каталога');
    if (it.owner && (it.owner.layer !== req.layer || it.owner.scopeId !== req.scopeId))
      throw new OrgApiError('invalid_request', 'Этот пункт принадлежит другому уровню: скопируйте его');
    if (entries.some((e) => e.itemId === req.itemId && e.layer === req.layer && e.scopeId === req.scopeId))
      throw new OrgApiError('exists', 'exists');
    serial += 1;
    const created: Entry = {
      entryId: `e${serial}`,
      itemId: req.itemId,
      layer: req.layer,
      scopeId: req.scopeId,
      pinned: !!req.pinned,
      enabled: req.enabled ?? true,
    };
    entries = [...entries, created];
    machineCurrent = false;
    return later(rowOf(ctx.role, created), 450);
  },

  envPatch: async (_org, entryId, req: PatchEntryRequest) => {
    const e = findEntry(entryId);
    guard(ctx.role, e.layer, e.scopeId);
    if (req.enabled !== undefined) e.enabled = req.enabled;
    if (req.pinned !== undefined) e.pinned = req.pinned;
    if (req.groupId !== undefined) {
      if (req.groupId !== '' && findGroup(req.groupId).layer !== e.layer)
        throw new OrgApiError('invalid_request', 'Группа другого уровня');
      e.groupId = req.groupId;
    }
    machineCurrent = false;
    return later(rowOf(ctx.role, e), 350);
  },

  envRemove: async (_org, entryId) => {
    const e = findEntry(entryId);
    guard(ctx.role, e.layer, e.scopeId);
    entries = entries.filter((x) => x.entryId !== entryId);
    machineCurrent = false;
    return later(undefined, 350);
  },

  envCatalog: async (_org, target, kind) => {
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    const list = [...items.values()].filter((it) => !it.archived && (!kind || it.kind === kind));
    return later(
      list.map((it) => catalogOf(it, target.layer, target.scopeId)),
      240,
    );
  },

  catalogAdd: async (_org, req: AddCatalogRequest) => {
    const owner = req.owner ?? null;
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    if (ctx.role === 'lead' && !(owner && owner.layer === 'team' && owner.scopeId === MY_TEAM))
      throw new OrgApiError('forbidden', 'forbidden', 'org_admin');
    const name = req.name.trim();
    if (
      [...items.values()].some(
        (i) => !i.archived && i.kind === req.kind && i.name.toLowerCase() === name.toLowerCase(),
      )
    )
      throw new OrgApiError('name_taken', 'Такое название в каталоге уже есть');
    refuseBrew(req.kind, req.source);
    refuseSecrets(ctx.role, req.kind, req.source);
    serial += 1;
    const it: Item = {
      id: `i-n${serial}`,
      kind: req.kind,
      name,
      hint: req.hint ?? '',
      source: req.source,
      owner,
      archived: false,
    };
    items.set(it.id, it);
    return later(catalogOf(it), 300);
  },

  catalogPatch: async (_org, itemId, req: PatchCatalogRequest) => {
    const it = findItem(itemId);
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    if (ctx.role === 'lead' && !(it.owner?.layer === 'team' && it.owner.scopeId === MY_TEAM))
      throw new OrgApiError('forbidden', 'forbidden', 'org_admin');
    if (req.name !== undefined) {
      const n = req.name.trim().toLowerCase();
      if (
        [...items.values()].some(
          (i) => i.id !== it.id && !i.archived && i.kind === it.kind && i.name.toLowerCase() === n,
        )
      )
        throw new OrgApiError('name_taken', 'Такое название в каталоге уже есть');
      it.name = req.name.trim();
    }
    if (req.source) {
      refuseBrew(it.kind, req.source);
      refuseSecrets(ctx.role, it.kind, req.source);
      it.source = req.source;
    }
    if (req.hint !== undefined) it.hint = req.hint;
    if (req.archived !== undefined) it.archived = req.archived;
    machineCurrent = false;
    return later(catalogOf(it), 400);
  },

  catalogDelete: async (_org, itemId) => {
    findItem(itemId).archived = true;
    return later(undefined, 300);
  },

  envOrder: async (_org, req) => {
    guard(ctx.role, req.layer, req.scopeId);
    const own = entries.filter((e) => e.layer === req.layer && e.scopeId === req.scopeId);
    const scripts = own.filter((e) => items.get(e.itemId)?.kind === 'script').map((e) => e.entryId);
    const same = scripts.length === req.entryIds.length && scripts.every((id) => req.entryIds.includes(id));
    if (!same) throw new OrgApiError('invalid_request', 'Нужны все скрипты уровня, каждый один раз');
    const queue = [...req.entryIds];
    const rebuilt = own.map((e) => (scripts.includes(e.entryId) ? findEntry(queue.shift() ?? e.entryId) : e));
    entries = [...entries.filter((e) => !(e.layer === req.layer && e.scopeId === req.scopeId)), ...rebuilt];
    machineCurrent = false;
    return later(req.entryIds, 450);
  },

  envRuns: async (_org, target, errorsOnly) => {
    if (ctx.role === 'member') throw new OrgApiError('forbidden', 'forbidden', 'team_lead');
    void target;
    const list = runs.filter((r) => !errorsOnly || r.steps.some((s) => s.status === 'error'));
    return later(list.map(bare), 260);
  },

  envRun: async (_org, runId) => {
    const r = runs.find((x) => x.id === runId);
    if (!r) throw new OrgApiError('not_found', 'run not found');
    return later(withLogs(r), 120);
  },

  envTry: async (_org, req) => {
    guard(ctx.role, req.layer, req.scopeId);
    const id = startSandbox(ctx, req.layer, req.scopeId, req.draft, MY_MACHINE);
    return later({ runId: id, machine: MY_MACHINE }, 300);
  },

  meMachines: async () => later([myMachine()], 200),

  meRun: async (req) => {
    const id = startMine(req.mode);
    return later({ runs: [{ runId: id, machineId: MY_MACHINE.id }] }, 300);
  },

  meRuns: async () =>
    later(
      runs.filter((r) => r.machine.id === MY_MACHINE.id || r.person.id === VIEWERS.member.id).map(bare),
      250,
    ),

  envMine: async (): Promise<MeEnvResponse> => {
    const mine = entries.filter(
      (e) =>
        e.enabled &&
        (e.layer === 'org' || e.layer === 'tenant' || (e.layer === 'team' && e.scopeId === MY_TEAM)),
    );
    const m = myMachine();
    const groups = mineGroups(mine);
    return later({
      space: { id: TENANT_ID, slug: 'product', name: tenantName() },
      org: { id: ORG_ID, name: 'Акме' },
      items: mine.map(mineOf),
      groups,
      presets: minePresets(groups),
      outdated: !m.current || m.failed > 0,
      machines: [m],
    });
  },

  envMineGroup: async (groupId, on) => {
    const g = findGroup(groupId);
    if (g.required) throw new OrgApiError('locked', 'Группа обязательная');
    groupOn.set(g.id, on);
    machineCurrent = false;
    const n = entries.filter((e) => e.groupId === g.id).length;
    return later(
      {
        id: g.id,
        name: g.name,
        hint: g.hint,
        layer: g.layer,
        required: false,
        on,
        canToggle: true,
        items: n,
        builtIn: g.builtIn,
      },
      300,
    );
  },

  envGroupAdd: async (_org, req: AddGroupRequest) => {
    guard(ctx.role, req.layer, req.scopeId);
    const name = req.name.trim();
    if (!name) throw new OrgApiError('invalid_request', 'Нужно название');
    if (groups.some((g) => g.layer === req.layer && g.scopeId === req.scopeId && g.name === name))
      throw new OrgApiError('name_taken', 'Такая группа уже есть');
    serial += 1;
    const g: Group = {
      id: `g${serial}`,
      layer: req.layer,
      scopeId: req.scopeId,
      name,
      hint: req.hint ?? '',
      required: !!req.required,
      defaultOn: req.defaultOn ?? false,
      position: groups.length,
      builtIn: false,
    };
    groups = [...groups, g];
    return later(groupView(g), 300);
  },

  envGroupPatch: async (_org, groupId, req) => {
    const g = findGroup(groupId);
    if (g.builtIn)
      throw new OrgApiError('built_in', 'Группу платформы в хабе не меняют: каждый включает её у себя');
    guard(ctx.role, g.layer, g.scopeId);
    if (req.name !== undefined) g.name = req.name.trim() || g.name;
    if (req.hint !== undefined) g.hint = req.hint;
    if (req.required !== undefined) g.required = req.required;
    if (req.defaultOn !== undefined) g.defaultOn = req.defaultOn;
    if (req.position !== undefined) g.position = req.position;
    machineCurrent = false;
    return later(groupView(g), 300);
  },

  envGroupRemove: async (_org, groupId) => {
    const g = findGroup(groupId);
    if (g.builtIn)
      throw new OrgApiError('built_in', 'Группу платформы в хабе не меняют: каждый включает её у себя');
    guard(ctx.role, g.layer, g.scopeId);
    groups = groups.filter((x) => x.id !== groupId);
    for (const p of presets) p.groupIds = p.groupIds.filter((id) => id !== groupId);
    for (const e of entries) if (e.groupId === groupId) e.groupId = '';
    machineCurrent = false;
    return later(undefined, 300);
  },

  envPresetAdd: async (_org, req: AddPresetRequest) => {
    guard(ctx.role, req.layer, req.scopeId);
    const name = req.name.trim();
    if (!name) throw new OrgApiError('invalid_request', 'Нужно название');
    if (presets.some((p) => p.layer === req.layer && p.scopeId === req.scopeId && p.name === name))
      throw new OrgApiError('name_taken', 'Такой пресет уже есть');
    const groupIds = req.groupIds ?? [];
    checkPresetGroups(req.layer, req.scopeId, groupIds);
    serial += 1;
    const p: Preset = {
      id: `p${serial}`,
      layer: req.layer,
      scopeId: req.scopeId,
      name,
      hint: req.hint ?? '',
      groupIds,
      position: presets.length,
    };
    presets = [...presets, p];
    return later(presetView(p), 300);
  },

  envPresetPatch: async (_org, presetId, req) => {
    const p = findPreset(presetId);
    guard(ctx.role, p.layer, p.scopeId);
    const name = req.name?.trim();
    if (
      name &&
      presets.some((o) => o.id !== p.id && o.layer === p.layer && o.scopeId === p.scopeId && o.name === name)
    )
      throw new OrgApiError('name_taken', 'Такой пресет уже есть');
    if (req.groupIds) checkPresetGroups(p.layer, p.scopeId, req.groupIds);
    if (name) p.name = name;
    if (req.hint !== undefined) p.hint = req.hint;
    if (req.groupIds) p.groupIds = req.groupIds;
    if (req.position !== undefined) p.position = req.position;
    return later(presetView(p), 300);
  },

  envPresetRemove: async (_org, presetId) => {
    const p = findPreset(presetId);
    guard(ctx.role, p.layer, p.scopeId);
    presets = presets.filter((x) => x.id !== presetId);
    return later(undefined, 300);
  },

  envMinePreset: async (presetId) => {
    const p = findPreset(presetId);
    const mine = entries.filter(
      (e) =>
        e.enabled &&
        (e.layer === 'org' || e.layer === 'tenant' || (e.layer === 'team' && e.scopeId === MY_TEAM)),
    );
    for (const g of mineGroups(mine)) {
      if (!g.required && g.layer === p.layer) groupOn.set(g.id, p.groupIds.includes(g.id));
    }
    machineCurrent = false;
    const groups = mineGroups(mine);
    return later({ presets: minePresets(groups), groups }, 300);
  },

  envApply: async (_org, req: ApplyRequest) => {
    guard(ctx.role, req.layer, req.scopeId);
    // Three machines: two online, one asleep.
    const machines = req.layer === 'team' ? 1 : 3;
    const started = req.when === 'now' ? machines - 1 : 0;
    if (started > 0) machineCurrent = true;
    return later({ machines, started, waiting: machines - started }, 400);
  },

  envMineSet: async (itemId, off) => {
    const e = entries.find((x) => x.itemId === itemId);
    if (!e) throw new OrgApiError('not_found', 'item not found');
    return later({ ...mineOf(e), off }, 300);
  },
});
