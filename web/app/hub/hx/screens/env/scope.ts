// Which layers the screen shows and which one the person is setting up: the chain organization → tenant → team, the
// names of a layer in the forms the board uses («в команде Фронт», «из тенанта Продукт»), the picker's list. Pure.
import type {
  CatalogKind,
  EnvEntryRow,
  EnvLayer,
  EnvLayerKind,
  EnvResponse,
  MeEnvResponse,
} from '../../../../hub-api/env';
import type { Role } from '../../../org/types';
import { plural } from './format';
import { GROUP_ORDER } from './model';

/** A layer: its kind and the id of its organization, tenant or team. */
export interface Target {
  layer: EnvLayerKind;
  scopeId: string;
}

export const same = (a: Target, b: Target): boolean => a.layer === b.layer && a.scopeId === b.scopeId;
export const keyOf = (t: Target): string => `${t.layer}:${t.scopeId}`;

export interface LayerMeta {
  kind: EnvLayerKind;
  initial: string;
  /** «Фронт», «Продукт», «Тиксли». */
  short: string;
  /** The picker's and the head's name: «Вся организация», «Тенант Продукт», «Команда Фронт». */
  pick: string;
  /** in the accusative: «в команду Фронт» */
  acc: string;
  /** the genitive: «из команды Фронт» */
  gen: string;
  /** the locative: «в команде Фронт» */
  loc: string;
}

const PREFIX = /^(Организация|Тенант|Команда)\s+/;

export const metaOf = (l: Pick<EnvLayer, 'layer' | 'title'>): LayerMeta => {
  const short = l.title.replace(PREFIX, '').trim() || l.title;
  const initial = short.slice(0, 1).toUpperCase() || '·';
  if (l.layer === 'org')
    return {
      kind: 'org',
      initial,
      short,
      pick: 'Вся организация',
      acc: 'организацию',
      gen: 'организации',
      loc: 'организации',
    };
  if (l.layer === 'tenant')
    return {
      kind: 'tenant',
      initial,
      short,
      pick: `Тенант ${short}`,
      acc: `тенант ${short}`,
      gen: `тенанта ${short}`,
      loc: `тенанте ${short}`,
    };
  return {
    kind: 'team',
    initial,
    short,
    pick: `Команда ${short}`,
    acc: `команду ${short}`,
    gen: `команды ${short}`,
    loc: `команде ${short}`,
  };
};

/** The layers a target takes into its environment, from the organization down to itself. */
export const chainOf = (layers: readonly EnvLayer[], t: Target): EnvLayer[] => {
  const org = layers.find((l) => l.layer === 'org');
  const me = layers.find((l) => same(l, t));
  if (!me) return org ? [org] : [];
  if (me.layer === 'org') return [me];
  const tenant =
    me.layer === 'tenant' ? me : layers.find((l) => l.layer === 'tenant' && l.scopeId === me.tenantId);
  return [org, tenant, me.layer === 'team' ? me : null].filter((l): l is EnvLayer => l != null);
};

/** The layers the person may change; a member has none. */
export const editableLayers = (layers: readonly EnvLayer[]): EnvLayer[] => layers.filter((l) => !l.locked);

/** Where the screen starts: the organization for an admin, the lead's own team for a lead, the member's team. */
export const firstTarget = (
  layers: readonly EnvLayer[],
  role: Role,
  teamId: string | null,
): Target | null => {
  const mine = editableLayers(layers);
  const own = layers.find((l) => l.layer === 'team' && l.scopeId === teamId);
  if (role === 'admin') {
    const org = layers.find((l) => l.layer === 'org' && !l.locked);
    return org ?? mine[0] ?? null;
  }
  if (own) return own;
  return mine[0] ?? layers.find((l) => l.layer === 'team') ?? layers[0] ?? null;
};

// ── the rows of a layer ───────────────────────────────────────────────────────────────────────────────────────────

export interface Block {
  kind: CatalogKind;
  rows: EnvEntryRow[];
}

/** The rows of a layer by kind, in the order of the board; a kind with nothing shown is not there. */
export const blocksOf = (rows: readonly EnvEntryRow[], q: string): Block[] => {
  const needle = q.trim().toLowerCase();
  const shown = rows
    .filter((r) => r.enabled)
    .filter((r) => needle === '' || `${r.name} ${r.hint}`.toLowerCase().includes(needle));
  return GROUP_ORDER.map((kind) => ({ kind, rows: shown.filter((r) => r.kind === kind) })).filter(
    (b) => b.rows.length > 0,
  );
};

/** Layers a member sees: what `/api/me/env` gives, in the shape of the layers (nothing there is theirs to change). */
export const layersOfMe = (me: MeEnvResponse): EnvLayer[] => {
  const out: EnvLayer[] = [];
  for (const it of me.items) {
    let l = out.find((x) => x.layer === it.layer && x.scopeId === it.scopeId);
    if (!l) {
      l = {
        layer: it.layer,
        scopeId: it.scopeId,
        tenantId: it.layer === 'org' ? '' : me.space.id,
        title: it.layerTitle,
        subtitle: '',
        locked: true,
        callsWeek: null,
        counts: { skill: 0, mcp: 0, repo: 0, doc: 0 },
        items: [],
      };
      out.push(l);
    }
    l.items.push({
      entryId: it.entryId,
      itemId: it.itemId,
      kind: it.kind,
      name: it.name,
      hint: it.hint,
      audience: { mode: 'tenant', teams: [] },
      pinned: it.pinned,
      locked: true,
      lockedReason: 'no_rights',
      enabled: true,
      callsWeek: null,
      spark14: [],
      lastUsedAt: null,
      unused: null,
      delivery: null,
    });
  }
  const rank: Record<EnvLayerKind, number> = { org: 0, tenant: 1, team: 2 };
  return out.sort((a, b) => rank[a.layer] - rank[b.layer]);
};

// ── the picker ────────────────────────────────────────────────────────────────────────────────────────────────────

export interface PickRow {
  head?: string;
  more?: string;
  target?: Target;
  kind?: EnvLayerKind;
  initial?: string;
  name?: string;
  sub?: string;
}

const MANY = 6;

/** The list of «Настраиваете» and «Куда»: an admin sees the organization, tenants and teams; a lead their teams. */
export const pickList = (view: Pick<EnvResponse, 'layers' | 'teams'>, role: Role, q: string): PickRow[] => {
  const needle = q.trim().toLowerCase();
  const match = (n: string): boolean => needle === '' || n.toLowerCase().includes(needle);
  const out: PickRow[] = [];
  const layers = editableLayers(view.layers);
  const item = (l: EnvLayer, sub: string): PickRow => {
    const m = metaOf(l);
    return {
      target: { layer: l.layer, scopeId: l.scopeId },
      kind: m.kind,
      initial: m.initial,
      name: m.pick === 'Вся организация' ? m.pick : m.short,
      sub,
    };
  };
  const tenantName = (id: string): string => {
    const t = view.layers.find((l) => l.layer === 'tenant' && l.scopeId === id);
    return t ? metaOf(t).short : '';
  };
  const teams = layers.filter((l) => l.layer === 'team');
  const hits = teams.filter((l) => match(metaOf(l).short));
  const cut = needle === '' && hits.length > MANY ? hits.slice(0, MANY - 1) : hits;
  const rest = hits.length - cut.length;
  const moreText = (n: number): string =>
    `и ещё ${n} ${plural(n, 'команда', 'команды', 'команд')} — начните вводить название`;

  if (role === 'admin') {
    const org = layers.find((l) => l.layer === 'org');
    if (org && match('Вся организация')) out.push({ head: 'Организация' }, item(org, ''));
    const tenants = layers.filter((l) => l.layer === 'tenant' && match(metaOf(l).short));
    if (tenants.length) {
      out.push({ head: 'Тенанты' });
      for (const t of tenants) {
        const n = view.teams.filter((x) => x.tenantId === t.scopeId).length;
        out.push(item(t, n === 0 ? '' : `${n} ${plural(n, 'команда', 'команды', 'команд')}`));
      }
    }
    if (cut.length) {
      out.push({ head: 'Команды' });
      for (const t of cut) out.push(item(t, tenantName(t.tenantId)));
    }
    if (rest > 0) out.push({ more: moreText(rest) });
  } else {
    if (cut.length) out.push({ head: 'Ваши команды' });
    for (const t of cut) out.push(item(t, tenantName(t.tenantId)));
    if (rest > 0) out.push({ more: moreText(rest) });
  }
  if (needle !== '' && !out.some((r) => r.target)) out.push({ more: 'Ничего не нашлось' });
  return out;
};

/** Whether the picker has a search field at all: a lead with a few teams has no need for one. */
export const pickHasSearch = (view: Pick<EnvResponse, 'layers'>, role: Role): boolean =>
  role === 'admin' || editableLayers(view.layers).filter((l) => l.layer === 'team').length > MANY;

/** The layers of the chain in which an item already is, the highest first. */
export const haveItem = (chain: readonly EnvLayer[], itemId: string): EnvLayer | null =>
  chain.find((l) => l.items.some((r) => r.itemId === itemId)) ?? null;
