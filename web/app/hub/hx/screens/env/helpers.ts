// What the environment screen computes from the hub's answer: texts, counts, the rows of a group, the drawer's draft and
// the patch it makes, and the optimistic changes of the view. Pure, so each piece is tested on its own.
import type {
  Audience,
  CatalogKind,
  EnvCard,
  EnvDelivery,
  EnvEntryRow,
  EnvLayer,
  EnvLayerKind,
  EnvResponse,
  EnvSummary,
  ItemDelivery,
  LockedReason,
  MeEnvItem,
  PatchEntryRequest,
  TeamRef,
} from '../../../../hub-api/env';
import { recount } from '../../../org/env-calc';

export type KindFilter = 'all' | CatalogKind;

export const KIND_FILTERS: { id: KindFilter; label: string }[] = [
  { id: 'all', label: 'Всё' },
  { id: 'skill', label: 'Скиллы' },
  { id: 'mcp', label: 'MCP' },
  { id: 'repo', label: 'Репы' },
  { id: 'doc', label: 'Доки' },
];

export const KIND_ICON: Record<CatalogKind, string> = {
  skill: 'sparkles',
  mcp: 'plug',
  repo: 'folder-git-2',
  doc: 'book-open',
};
export const KIND_WORD: Record<CatalogKind, string> = {
  skill: 'Скилл',
  mcp: 'MCP',
  repo: 'Репозиторий',
  doc: 'Документ',
};
export const KIND_COLOR: Record<CatalogKind, string> = {
  skill: '#8968f4',
  mcp: '#25a897',
  repo: '#3b82f6',
  doc: '#c8830b',
};
const KINDS: CatalogKind[] = ['skill', 'mcp', 'repo', 'doc'];

/** 12345 -> «12 345». */
export const fmtNum = (n: number): string => String(Math.round(n)).replace(/\B(?=(\d{3})+(?!\d))/g, ' ');

export const plural = (n: number, one: string, few: string, many: string): string => {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return many;
  if (b > 1 && b < 5) return few;
  return b === 1 ? one : many;
};

/** «3 скилла», «2 MCP», «1 репо», «2 док»: the short form under a layer's name. */
export const kindShort = (kind: CatalogKind, n: number): string => {
  if (kind === 'skill') return `${n} ${plural(n, 'скилл', 'скилла', 'скиллов')}`;
  if (kind === 'mcp') return `${n} MCP`;
  return `${n} ${kind === 'repo' ? 'репо' : 'док'}`;
};

/** «8 скиллов», «3 репы», «3 документа»: the long form of the summary tile. */
export const kindLong = (kind: CatalogKind, n: number): string => {
  if (kind === 'skill') return `${n} ${plural(n, 'скилл', 'скилла', 'скиллов')}`;
  if (kind === 'mcp') return `${n} MCP`;
  if (kind === 'repo') return `${n} ${plural(n, 'репа', 'репы', 'реп')}`;
  return `${n} ${plural(n, 'документ', 'документа', 'документов')}`;
};

/** When something was last called, in the words of the board: «сейчас», «5 мин назад», «вчера», «34 дня назад». */
export const lastUsed = (iso: string | null, now: number): string => {
  if (!iso) return 'не вызывался';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return 'не вызывался';
  const min = Math.floor((now - t) / 60_000);
  if (min < 2) return 'сейчас';
  if (min < 60) return `${min} мин назад`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} ч назад`;
  const d = Math.floor(h / 24);
  if (d < 2) return 'вчера';
  return `${d} ${plural(d, 'день', 'дня', 'дней')} назад`;
};

// ── the summary tiles ─────────────────────────────────────────────────────────────────────────────────────────────

export interface Part {
  weight: number;
  color?: string;
}
export type TileChart = { strip: Part[] } | { bars: number[] };
export interface Tile {
  tone: string;
  icon: string;
  label: string;
  value: string;
  chart: TileChart;
}

const machines = (n: number): string => `${n} ${plural(n, 'машине', 'машинах', 'машинах')}`;

/** How far the list got: how many of the screen's machines hold the current one. */
export const deliveryTile = (d: EnvDelivery, total: number): Tile => {
  const behind = Math.max(0, d.machines - d.current);
  let value: string;
  if (d.machines === 0) value = 'машин пока нет';
  else if (total === 0)
    value = `доставлять пока нечего · ${d.machines} ${plural(d.machines, 'машина', 'машины', 'машин')}`;
  else if (behind === 0) value = `${d.current} из ${d.machines} — у всех свежий список`;
  else if (d.reported === 0) value = `0 из ${d.machines} — машины ещё не отвечали`;
  else value = `${d.current} из ${d.machines} · ${behind} ещё не получили`;
  return {
    tone: '#22a06b',
    icon: 'server',
    label: 'Доехало до машин',
    value,
    chart: {
      strip: [
        { weight: d.current, color: '#22a06b' },
        { weight: Math.max(0, d.reported - d.current), color: '#c8830b' },
        { weight: Math.max(0, d.machines - d.reported) },
      ],
    },
  };
};

/** What went wrong with an item on the machines, in the board's words; null: nothing. */
export const deliveryIssue = (d: ItemDelivery | null): string | null => {
  if (!d) return null;
  if (d.error) return `не встало на ${machines(d.error)}`;
  if (d.pending) return `работает не полностью на ${machines(d.pending)}`;
  return null;
};

/** The card's line about the machines: where the item stands and why not, if not. */
export const deliveryText = (d: ItemDelivery | null, all: EnvDelivery): string => {
  if (all.machines === 0) return 'Машин пока нет — список доедет, когда машина подключится';
  if (!d)
    return all.current
      ? 'Машины со свежим списком об этом элементе пока не сообщили'
      : 'Машины ещё не получили свежий список';
  const parts = [`стоит на ${machines(d.ok)}`];
  if (d.pending) parts.push(`не полностью на ${machines(d.pending)}`);
  if (d.error) parts.push(`не встало на ${machines(d.error)}`);
  const head = parts.join(', ');
  return `${head.charAt(0).toUpperCase()}${head.slice(1)} из ${all.machines}`;
};

/** The calls of each of the 14 days over every row that is switched on. */
export const dailyCalls = (layers: readonly EnvLayer[]): number[] => {
  const out = Array.from({ length: 14 }, () => 0);
  for (const l of layers) {
    for (const r of l.items) {
      if (!r.enabled) continue;
      r.spark14.forEach((v, i) => {
        if (i < out.length) out[i] = (out[i] ?? 0) + v;
      });
    }
  }
  return out;
};

const joinNames = (names: string[]): string =>
  names.length < 2 ? (names[0] ?? '') : `${names.slice(0, -1).join(', ')} и ${names[names.length - 1]}`;

export const tilesOf = (view: EnvResponse): Tile[] => {
  const s: EnvSummary = view.summary;
  const kinds = KINDS.filter((k) => s.byKind[k] > 0);
  const total = s.total;
  return [
    {
      tone: '#8968f4',
      icon: 'layers',
      label: 'В окружении',
      value: total ? `${total}: ${kinds.map((k) => kindLong(k, s.byKind[k])).join(' · ')}` : 'пока ничего',
      chart: { strip: kinds.map((k) => ({ weight: s.byKind[k], color: KIND_COLOR[k] })) },
    },
    {
      tone: '#25a897',
      icon: 'activity',
      label: 'Вызовов за неделю',
      value:
        s.callsWeek === null
          ? 'данных о вызовах ещё нет'
          : `${fmtNum(s.callsWeek)}${s.top.length ? ` · чаще всех ${joinNames(s.top.map((t) => t.name))}` : ''}`,
      chart: { bars: dailyCalls(view.layers) },
    },
    {
      tone: '#c8830b',
      icon: 'moon',
      label: 'Не используются',
      value: s.unused === null ? 'данных пока нет' : `${s.unused} — месяц без вызовов, можно убрать`,
      chart: {
        strip: [
          { weight: s.unused ?? 0, color: '#c8830b' },
          { weight: Math.max(0, total - (s.unused ?? 0)) },
        ],
      },
    },
    deliveryTile(view.delivery, view.summary.total),
  ];
};

// ── rows and groups ───────────────────────────────────────────────────────────────────────────────────────────────

export const teamNameOf = (layer: EnvLayer, teams: readonly TeamRef[]): string =>
  teams.find((t) => t.id === layer.scopeId)?.name ?? layer.title.replace(/^Команда\s+/, '');

/** Who an entry reaches, in the board's words. */
export const whoText = (row: EnvEntryRow, layer: EnvLayer, teams: readonly TeamRef[]): string => {
  if (!row.enabled) return 'выключено';
  if (layer.layer === 'org') return 'все тенанты';
  if (layer.layer === 'team') return `команда ${teamNameOf(layer, teams)}`;
  if (row.audience.mode === 'teams') {
    const names = row.audience.teams.map((id) => teams.find((t) => t.id === id)?.name ?? id);
    return names.length ? names.join(', ') : 'никому';
  }
  return 'весь тенант';
};

export interface RowVM {
  entryId: string;
  itemId: string;
  kind: CatalogKind;
  icon: string;
  name: string;
  sub: string;
  /** What went wrong on the machines, shown instead of `sub`; `why` is the machine's own reason. */
  issue: string | null;
  why: string | null;
  who: string;
  off: boolean;
  zero: boolean;
  /** 0..100, one per day. */
  spark: number[];
  wk: string;
  last: string;
  stale: boolean;
  locked: boolean;
  lockedReason: LockedReason | null;
  layerKey: string;
}

export const layerKey = (l: Pick<EnvLayer, 'layer' | 'scopeId'>): string => `${l.layer}:${l.scopeId}`;

export const sparkHeights = (spark: readonly number[]): number[] => {
  const mx = Math.max(1, ...spark);
  return spark.map((v) => Math.round((v / mx) * 100));
};

export const rowVM = (
  row: EnvEntryRow,
  layer: EnvLayer,
  teams: readonly TeamRef[],
  now: number,
  withLayer: boolean,
): RowVM => ({
  entryId: row.entryId,
  itemId: row.itemId,
  kind: row.kind,
  icon: KIND_ICON[row.kind],
  name: row.name,
  sub: withLayer ? `${KIND_WORD[row.kind]} · ${layer.title}` : `${KIND_WORD[row.kind]} · ${row.hint}`,
  issue: row.enabled ? deliveryIssue(row.delivery) : null,
  why: row.enabled ? (row.delivery?.message ?? null) : null,
  who: whoText(row, layer, teams),
  off: !row.enabled,
  zero: !row.callsWeek,
  spark: sparkHeights(row.spark14),
  wk: row.callsWeek ? fmtNum(row.callsWeek) : '—',
  last: row.enabled ? lastUsed(row.lastUsedAt, now) : 'выключено',
  stale: row.enabled && row.callsWeek === 0,
  locked: row.locked,
  lockedReason: row.lockedReason,
  layerKey: layerKey(layer),
});

export const LOCK_TITLE: Record<LockedReason, string> = {
  pinned_by_org: 'Закреплено организацией — здесь не выключить',
  pinned_by_tenant: 'Закреплено тенантом — здесь не выключить',
  no_rights: 'Менять может руководитель слоя или админ',
};

export const matches = (row: EnvEntryRow, layer: EnvLayer, teams: readonly TeamRef[], q: string): boolean => {
  const needle = q.trim().toLowerCase();
  if (!needle) return true;
  return [row.name, row.hint, KIND_WORD[row.kind], whoText(row, layer, teams)].some((x) =>
    x.toLowerCase().includes(needle),
  );
};

export interface Group {
  key: string;
  layer: EnvLayer;
  i: number;
  color: string;
  initial: string;
  open: boolean;
  kinds: string;
  calls: string;
  rows: RowVM[];
}

const TEAM_COLORS = ['#8968f4', '#25a897', '#c8830b', '#d9534a'];

export const layerColor = (layer: EnvLayer, layers: readonly EnvLayer[]): string => {
  if (layer.layer === 'org') return '#7c7c86';
  if (layer.layer === 'tenant') return '#3b82f6';
  const n = layers.filter((l) => l.layer === 'team').findIndex((l) => l.scopeId === layer.scopeId);
  return TEAM_COLORS[Math.max(0, n) % TEAM_COLORS.length] ?? '#8968f4';
};

/** The second word of a layer's title: «Организация Акме» -> «А». */
export const layerInitial = (title: string): string =>
  (title.split(/\s+/)[1] ?? title).slice(0, 1).toUpperCase() || '·';

export interface Filter {
  kind: KindFilter;
  q: string;
}

export const isFiltering = (f: Filter): boolean => f.kind !== 'all' || f.q.trim() !== '';

/** The layers that have rows under the filter, each with its shown rows. A filter opens every group. */
export const groupsOf = (
  view: EnvResponse,
  f: Filter,
  openMap: Readonly<Record<string, boolean>>,
  now: number,
): Group[] => {
  const out: Group[] = [];
  view.layers.forEach((layer) => {
    const shown = layer.items.filter(
      (r) => (f.kind === 'all' || r.kind === f.kind) && matches(r, layer, view.teams, f.q),
    );
    if (!shown.length) return;
    const key = layerKey(layer);
    const by = KINDS.map((k) => [k, shown.filter((r) => r.enabled && r.kind === k).length] as const).filter(
      ([, n]) => n,
    );
    out.push({
      key,
      layer,
      i: out.length,
      color: layerColor(layer, view.layers),
      initial: layerInitial(layer.title),
      open: isFiltering(f) ? true : (openMap[key] ?? false),
      kinds: by.map(([k, n]) => kindShort(k, n)).join(' · '),
      calls: fmtNum(shown.reduce((n, r) => n + (r.enabled ? (r.callsWeek ?? 0) : 0), 0)),
      rows: shown.map((r) => rowVM(r, layer, view.teams, now, false)),
    });
  });
  return out;
};

/** Every shown row in one list, each saying which layer it is from. */
export const flatRows = (view: EnvResponse, f: Filter, now: number): RowVM[] =>
  view.layers.flatMap((layer) =>
    layer.items
      .filter((r) => (f.kind === 'all' || r.kind === f.kind) && matches(r, layer, view.teams, f.q))
      .map((r) => rowVM(r, layer, view.teams, now, true)),
  );

export const findRow = (view: EnvResponse, entryId: string): { layer: EnvLayer; row: EnvEntryRow } | null => {
  for (const layer of view.layers) {
    const row = layer.items.find((r) => r.entryId === entryId);
    if (row) return { layer, row };
  }
  return null;
};

// ── the drawer of one entry ───────────────────────────────────────────────────────────────────────────────────────

export type Mode = 'all' | 'teams' | 'off';
export interface Draft {
  mode: Mode;
  teams: string[];
  pinned: boolean;
}

export const draftOf = (row: Pick<EnvEntryRow, 'enabled' | 'audience' | 'pinned'>): Draft => ({
  mode: !row.enabled ? 'off' : row.audience.mode === 'teams' ? 'teams' : 'all',
  teams: row.audience.mode === 'teams' ? [...row.audience.teams] : [],
  pinned: row.pinned,
});

const sameSet = (a: readonly string[], b: readonly string[]): boolean =>
  a.length === b.length && [...a].sort().join('\u0000') === [...b].sort().join('\u0000');

export const draftChanged = (base: Draft, next: Draft): boolean =>
  base.mode !== next.mode ||
  base.pinned !== next.pinned ||
  (next.mode === 'teams' && !sameSet(base.teams, next.teams));

/** A draft the hub can take: «only some teams» names at least one. */
export const draftValid = (d: Draft): boolean => d.mode !== 'teams' || d.teams.length > 0;

/**
 * What to send for a draft: only the keys that changed (absent keys stay). The mode `off` is `enabled: false`, the entry
 * is kept; a team layer has no audience to pick.
 */
export const draftPatch = (
  row: Pick<EnvEntryRow, 'enabled' | 'audience' | 'pinned'>,
  next: Draft,
  layer: EnvLayerKind,
): PatchEntryRequest => {
  const patch: PatchEntryRequest = {};
  const want = next.mode !== 'off';
  if (want !== row.enabled) patch.enabled = want;
  if (want && layer !== 'team') {
    const audience: Audience =
      next.mode === 'teams' ? { mode: 'teams', teams: [...next.teams] } : { mode: 'tenant', teams: [] };
    const same =
      audience.mode === row.audience.mode &&
      (audience.mode !== 'teams' || sameSet(audience.teams, row.audience.teams));
    if (!same) patch.audience = audience;
  }
  if (next.pinned !== row.pinned) patch.pinned = next.pinned;
  return patch;
};

export const hasPatch = (p: PatchEntryRequest): boolean => Object.keys(p).length > 0;

export interface DayBar {
  h: number;
  label: string;
  title: string;
}

/** The bars of the card: the day's number under every second bar, «д.мм — N вызовов» on hover, a stub when no calls. */
export const dayBars = (bars: EnvCard['bars14'], callsWeek: number | null): DayBar[] => {
  const mx = Math.max(1, ...bars.map((b) => b.calls));
  return bars.map((b, i) => {
    const day = Number(b.day.slice(8, 10));
    return {
      h: callsWeek ? Math.round((b.calls / mx) * 100) : 3,
      label: i % 2 ? '' : String(day),
      title: `${day}.${b.day.slice(5, 7)} — ${b.calls} ${plural(b.calls, 'вызов', 'вызова', 'вызовов')}`,
    };
  });
};

// ── optimistic changes of the view ────────────────────────────────────────────────────────────────────────────────

const mapRow = (view: EnvResponse, entryId: string, f: (r: EnvEntryRow) => EnvEntryRow): EnvResponse =>
  recount({
    ...view,
    layers: view.layers.map((l) =>
      l.items.some((r) => r.entryId === entryId)
        ? { ...l, items: l.items.map((r) => (r.entryId === entryId ? f(r) : r)) }
        : l,
    ),
  });

/** A patch as the screen shows it before the hub answers. */
export const withPatch = (view: EnvResponse, entryId: string, patch: PatchEntryRequest): EnvResponse =>
  mapRow(view, entryId, (r) => ({
    ...r,
    enabled: patch.enabled ?? r.enabled,
    pinned: patch.pinned ?? r.pinned,
    audience: patch.audience ? { mode: patch.audience.mode, teams: patch.audience.teams ?? [] } : r.audience,
  }));

/** The row as the hub answered, in place of the one shown. */
export const withRow = (view: EnvResponse, row: EnvEntryRow): EnvResponse =>
  mapRow(view, row.entryId, () => row);

export const withoutEntry = (view: EnvResponse, entryId: string): EnvResponse =>
  recount({
    ...view,
    layers: view.layers.map((l) => ({ ...l, items: l.items.filter((r) => r.entryId !== entryId) })),
  });

/** A row put into a layer (a new one goes last). */
export const withAdded = (
  view: EnvResponse,
  target: { layer: EnvLayerKind; scopeId: string },
  row: EnvEntryRow,
): EnvResponse =>
  recount({
    ...view,
    layers: view.layers.map((l) =>
      l.layer === target.layer &&
      l.scopeId === target.scopeId &&
      !l.items.some((r) => r.entryId === row.entryId)
        ? { ...l, items: [...l.items, row] }
        : l,
    ),
  });

/** Whether the viewer may edit anything in a layer (the add button and the catalog's target). */
export const layerEditable = (l: EnvLayer): boolean => !l.locked;

// ── the member's own list ─────────────────────────────────────────────────────────────────────────────────────────

export const mineFiltered = (items: readonly MeEnvItem[], kind: KindFilter): MeEnvItem[] =>
  kind === 'all' ? [...items] : items.filter((i) => i.kind === kind);

export const withMineOff = (items: readonly MeEnvItem[], itemId: string, off: boolean): MeEnvItem[] =>
  items.map((i) => (i.itemId === itemId ? { ...i, off } : i));

/** Where the item comes from, as the small label on the row. */
export const mineSource = (i: Pick<MeEnvItem, 'layer' | 'layerTitle'>): string =>
  i.layer === 'org' ? 'Организация' : i.layer === 'team' ? 'Команда' : i.layerTitle;

// ── where the catalog adds to ─────────────────────────────────────────────────────────────────────────────────────

export interface Target {
  layer: EnvLayerKind;
  scopeId: string;
}

/**
 * The layer the «Из каталога» button puts items into: an admin looking at a tenant adds to the tenant, looking at the
 * whole organization to the organization; a lead adds to their team. A member adds nothing here.
 */
export const catalogTarget = (
  role: 'admin' | 'lead' | 'member',
  scope: { orgId: string; tenantId: string | null },
  teamId: string | null,
): Target | null => {
  if (role === 'member') return null;
  if (role === 'lead') return teamId ? { layer: 'team', scopeId: teamId } : null;
  return scope.tenantId
    ? { layer: 'tenant', scopeId: scope.tenantId }
    : { layer: 'org', scopeId: scope.orgId };
};

/** The entry a catalog item already has in the target layer, if any. */
export const entryIn = (view: EnvResponse, target: Target, itemId: string): EnvEntryRow | null =>
  view.layers
    .find((l) => l.layer === target.layer && l.scopeId === target.scopeId)
    ?.items.find((r) => r.itemId === itemId) ?? null;
