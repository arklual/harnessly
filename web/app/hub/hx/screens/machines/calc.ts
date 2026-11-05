// What the machines screen computes from the data, as pure functions: the states and texts of the rows, the tiles, the
// filters of the organization list, the stops of the resize sliders, the price lines. The components only draw them.
import type {
  MachineBusyKind,
  MachineGroup,
  MachinesList,
  MachinesSummary,
  Money,
  OrgMachine,
  ResourcePlan,
  RestartWhat,
} from '../../../org/api/machines';
import { ago, BUSY_TEXT, plural } from '../../../org/screens/machines/format';
import { fromUsd } from '../../../org/screens/overview/format';
import { HOT_PCT, isHot } from '../../charts/geometry';

export type RowSt = 'on' | 'idle' | 'off';

/** The three states the list draws; a machine switched off in the hub reads as not reachable. */
export const rowSt = (m: OrgMachine): RowSt =>
  m.state === 'offline' || m.state === 'disabled' ? 'off' : m.state === 'idle' ? 'idle' : 'on';

export const rowStLabel = (m: OrgMachine, now: number): string => {
  if (m.state === 'disabled') return 'отключена';
  const st = rowSt(m);
  if (st === 'on') return 'на связи';
  const t = ago(m.since, now);
  return `${st === 'idle' ? 'простаивает' : 'не на связи'}${t ? ` ${t}` : ''}`;
};

/** «16 ядер · 64 ГБ», for a laptop «M3 Pro · 36 ГБ»; what is unknown (0) is left out. */
export const hwLine = (m: OrgMachine): string => {
  const { cores, ramGb, note } = m.hw;
  const first = note || (cores > 0 ? `${cores} ${plural(cores, 'ядро', 'ядра', 'ядер')}` : '');
  return [first, ramGb > 0 ? `${ramGb} ГБ` : ''].filter(Boolean).join(' · ');
};

/** Whose it is or what it is for: the owner of a personal machine, the purpose of a shared one. */
export const whoOf = (m: OrgMachine): string => (m.kind === 'personal' ? m.ownerName : m.purpose) ?? '';

export type RowMode = 'team' | 'group' | 'flat-tenant' | 'flat-org';

/** The second line of a row: it differs by the view (the board, `rowSub`). */
export const rowSub = (m: OrgMachine, mode: RowMode): string => {
  const who = whoOf(m);
  const hw = hwLine(m);
  if (mode === 'flat-org') return [m.tenantName, who].filter(Boolean).join(' · ');
  if (mode === 'group') return [who, hw].filter(Boolean).join(' · ');
  const lead = m.kind === 'shared' ? (who ? `общая · ${who}` : 'общая') : who;
  return [lead, hw].filter(Boolean).join(' · ');
};

/** «7,7 тыс ₽ · 41 чат»; «—» while the money is not visible to the role and while nothing ran. */
export const weekText = (m: OrgMachine): string => {
  if (m.weekCostUsd == null || m.weekChats === 0) return '—';
  const usd = fromUsd(m.weekCostUsd);
  return m.weekChats == null ? usd : `${usd} · ${m.weekChats} ${plural(m.weekChats, 'чат', 'чата', 'чатов')}`;
};

/** What the table shows in the CPU and memory columns: nothing while the machine does not answer. */
export const liveLoad = (m: OrgMachine, v: number | null): number | null => (rowSt(m) === 'off' ? null : v);

/** The chats column: a number while the machine answers, a dash otherwise. */
export const chatsText = (m: OrgMachine): string =>
  rowSt(m) === 'on' && m.chats != null ? String(m.chats) : '—';

export const verText = (m: OrgMachine): string =>
  m.runnerVersion ? (m.runnerOutdated ? `${m.runnerVersion} · старый` : m.runnerVersion) : '—';

/** The group's money for the week in the hub's currency: «19,9 тыс ₽»; null when hidden. */
export const groupWeek = (g: MachineGroup): string | null =>
  g.weekCostUsd == null ? null : fromUsd(g.weekCostUsd);

/** «3 машины», always «машин» in the organization list (the board's wording). */
export const machinesWord = (n: number, org: boolean): string =>
  org ? 'машин' : plural(n, 'машина', 'машины', 'машин');

export const num = (n: number): string => n.toLocaleString('ru-RU');

// ── Search and filters ──────────────────────────────────────────────────────────────────────────────────────────────

const has = (hay: string, needle: string) => hay.toLowerCase().includes(needle);

export const machineHit = (m: OrgMachine, q: string): boolean => {
  const needle = q.trim().toLowerCase();
  if (!needle) return true;
  return [m.name, whoOf(m), m.ownerName ?? '', m.teamName ?? '', m.tenantName].some((s) => has(s, needle));
};

const groupHit = (g: MachineGroup, q: string) => {
  const needle = q.trim().toLowerCase();
  return !needle || has(g.name, needle) || has(g.sub, needle);
};

export interface ShownGroup {
  group: MachineGroup;
  rows: OrgMachine[];
}

/** The groups a search leaves: a group whose own name matches keeps all its rows, otherwise only the matching ones. */
export const searchGroups = (groups: readonly MachineGroup[], q: string): ShownGroup[] =>
  groups.flatMap((group) => {
    if (!q.trim()) return [{ group, rows: group.machines }];
    if (groupHit(group, q)) return [{ group, rows: group.machines }];
    const rows = group.machines.filter((m) => machineHit(m, q));
    return rows.length > 0 ? [{ group, rows }] : [];
  });

/** The rows of the flat view, with the problems first. */
export const flatRows = (groups: readonly MachineGroup[], q: string): OrgMachine[] => {
  const rank = (m: OrgMachine) => (rowSt(m) === 'off' ? 0 : rowSt(m) === 'idle' ? 1 : 2);
  return groups
    .flatMap((g) => g.machines)
    .filter((m) => machineHit(m, q))
    .sort((a, b) => rank(a) - rank(b) || a.name.localeCompare(b.name, 'ru'));
};

export type TenantMode = 'all' | 'problems' | 'pinned' | 'pick';

export interface TenantFilter {
  mode: TenantMode;
  pick: readonly string[];
}

export const filterTenants = (groups: readonly MachineGroup[], f: TenantFilter): MachineGroup[] =>
  groups.filter((g) => {
    if (f.mode === 'problems') return g.offline + g.idle > 0;
    if (f.mode === 'pinned') return g.pinned === true;
    if (f.mode === 'pick') return f.pick.includes(g.key);
    return true;
  });

export const filterLabel = (f: TenantFilter): string =>
  f.mode === 'problems'
    ? 'С проблемами'
    : f.mode === 'pinned'
      ? 'Закреплённые'
      : f.mode === 'pick'
        ? `Тенанты: ${f.pick.length}`
        : 'Все тенанты';

/** The tenants of the filter's list: by name, at most 14, those matching the filter's own search. */
export const filterOptions = (groups: readonly MachineGroup[], q: string): MachineGroup[] => {
  const needle = q.trim().toLowerCase();
  return groups
    .filter((g) => !needle || has(g.name, needle))
    .sort((a, b) => a.name.localeCompare(b.name, 'ru'))
    .slice(0, 14);
};

/** The dot of a tenant in the filter: red when something is offline, amber when idle, green otherwise. */
export const groupSt = (g: Pick<MachineGroup, 'offline' | 'idle'>): RowSt =>
  g.offline > 0 ? 'off' : g.idle > 0 ? 'idle' : 'on';

export const toggleTenant = (f: TenantFilter, key: string): TenantFilter => {
  const pick = f.pick.includes(key) ? f.pick.filter((k) => k !== key) : [...f.pick, key];
  return { mode: pick.length > 0 ? 'pick' : 'all', pick };
};

/** How many groups and flat rows are drawn before the sentinel loads the next page. */
export const groupLimit = (more: number): number => 12 + 10 * more;
export const rowLimit = (more: number): number => 25 + 25 * more;

// ── Tiles ───────────────────────────────────────────────────────────────────────────────────────────────────────────

export interface Tile {
  icon: string;
  tone: string;
  label: string;
  value: string;
  bars?: number[];
  strip?: { w: number; color?: string }[];
}

const GREEN = '#24ae56';
const VIOLET = '#8968f4';
const TEAL = '#25a897';
const AMBER = '#c8830b';
const RED = '#d9534a';

const machineWord = (n: number) => plural(n, 'машина', 'машины', 'машин');

const idleDays = (groups: readonly MachineGroup[], now: number): number | null => {
  const idle = groups.flatMap((g) => g.machines).filter((m) => m.state === 'idle' && m.since);
  if (idle.length === 0) return null;
  const oldest = Math.min(...idle.map((m) => Date.parse(m.since ?? '')));
  const days = Math.floor((now - oldest) / 86_400_000);
  return days >= 1 ? days : null;
};

const hotDisks = (groups: readonly MachineGroup[]) =>
  groups.flatMap((g) => g.machines).filter((m) => isHot(m.disk)).length;

/** The four tiles of a tenant (the board, `isMachTenant`). */
export const tenantTiles = (s: MachinesSummary, groups: readonly MachineGroup[], now: number): Tile[] => {
  const days = idleDays(groups, now);
  const idle =
    s.idleWeek === 0
      ? ''
      : s.idleWeek === 1
        ? ` · одна простаивает${days ? ` ${days} ${plural(days, 'день', 'дня', 'дней')}` : ''}`
        : ` · ${s.idleWeek} простаивают`;
  const today = s.chatsToday == null ? '—' : num(s.chatsToday);
  const load =
    s.cpuMean == null
      ? '—'
      : `в среднем ${s.cpuMean}%${s.hottest ? ` · ${s.hottest.name} под ${s.hottest.pct}%` : ''}`;
  const disk = s.diskHot && s.diskHot.pct >= HOT_PCT ? `диск ${s.diskHot.name} ${s.diskHot.pct}%` : '';
  const old =
    s.outdatedRunners > 0
      ? `${s.outdatedRunners} ${plural(s.outdatedRunners, 'раннер устарел', 'раннера устарели', 'раннеров устарели')}`
      : '';
  const attention = [disk, old].filter(Boolean).join(' · ') || 'всё в порядке';
  const redW = disk ? Math.max(1, hotDisks(groups)) : 0;
  return [
    {
      icon: 'server',
      tone: GREEN,
      label: 'На связи',
      value: `${s.online} из ${s.total}${idle}`,
      bars: s.onlineSeries,
    },
    {
      icon: 'messages-square',
      tone: VIOLET,
      label: 'Чаты сейчас',
      value: `${s.chatsNow} ${plural(s.chatsNow, 'идёт', 'идут', 'идут')} · ${today} за сегодня`,
      bars: s.chatsSeries,
    },
    { icon: 'gauge', tone: TEAL, label: 'Нагрузка', value: load, bars: s.loadSeries },
    {
      icon: 'triangle-alert',
      tone: AMBER,
      label: 'Требует внимания',
      value: attention,
      strip: [
        { w: s.outdatedRunners, color: AMBER },
        { w: redW, color: RED },
        { w: Math.max(0, s.total - s.outdatedRunners - redW) },
      ],
    },
  ];
};

/** The four tiles of the organization (the board, `isMachOrg`). */
export const orgTiles = (s: MachinesSummary, groups: readonly MachineGroup[]): Tile[] => {
  const tIdle = groups.filter((g) => g.idle > 0).length;
  const tOff = s.tenantsOffline || groups.filter((g) => g.offline > 0).length;
  const tenants = (n: number) => `${n} ${plural(n, 'тенанте', 'тенантах', 'тенантах')}`;
  const today = s.chatsToday == null ? '—' : num(s.chatsToday);
  return [
    {
      icon: 'server',
      tone: GREEN,
      label: 'На связи',
      value: `${num(s.online)} из ${num(s.total)} машин`,
      bars: s.onlineSeries,
    },
    {
      icon: 'messages-square',
      tone: VIOLET,
      label: 'Чаты сейчас',
      value: `${num(s.chatsNow)} ${plural(s.chatsNow, 'идёт', 'идут', 'идут')} · ${today} за сегодня`,
      bars: s.chatsSeries,
    },
    {
      icon: 'clock',
      tone: AMBER,
      label: 'Простаивают неделю',
      value: `${num(s.idleWeek)} ${machineWord(s.idleWeek)} в ${tenants(tIdle)}`,
      strip: [{ w: s.idleWeek, color: AMBER }, { w: Math.max(0, s.total - s.idleWeek) }],
    },
    {
      icon: 'wifi-off',
      tone: RED,
      label: 'Не на связи',
      value: `${num(s.offline)} ${machineWord(s.offline)} в ${tenants(tOff)}`,
      strip: [{ w: s.offline, color: RED }, { w: Math.max(0, s.total - s.offline) }],
    },
  ];
};

/** The lead of a tenant's screen: «Тенант Продукт · 8 машин: 6 личных и 2 общие». */
export const tenantLead = (name: string, machines: readonly OrgMachine[]): string => {
  const personal = machines.filter((m) => m.kind === 'personal').length;
  const shared = machines.length - personal;
  const parts = [
    personal > 0 ? `${personal} ${plural(personal, 'личная', 'личные', 'личных')}` : '',
    shared > 0 ? `${shared} ${plural(shared, 'общая', 'общие', 'общих')}` : '',
  ].filter(Boolean);
  return `Тенант ${name} · ${machines.length} ${machineWord(machines.length)}${parts.length ? `: ${parts.join(' и ')}` : ''}`;
};

// ── Resources: stops of the sliders, prices ─────────────────────────────────────────────────────────────────────────

export interface Stops {
  vals: number[];
  /** The index of the first value the person may pick (memory and disk cannot go below what is used). */
  firstOk: number;
}

/** The first stop at or above `floor`; when nothing reaches it, the last one. */
export const stopsOf = (vals: readonly number[], floor: number): Stops => {
  const i = vals.findIndex((v) => v >= floor);
  return { vals: [...vals], firstOk: i < 0 ? Math.max(0, vals.length - 1) : i };
};

/** The stops with the machine's current value among them (a size the plans do not list is still where it stands). */
export const withValue = (vals: readonly number[], v: number): number[] =>
  v > 0 && !vals.includes(v) ? [...vals, v].sort((a, b) => a - b) : [...vals];

/** Memory may not go below what is used plus a margin of 15%. */
export const ramFloor = (usedGb: number): number => Math.ceil(usedGb * 1.15);

/** A pointer at `t` (0..1) along the track picks this stop; the locked ones are skipped. */
export const snapStop = (t: number, n: number, firstOk: number): number =>
  Math.max(firstOk, Math.round(Math.max(0, Math.min(1, t)) * Math.max(0, n - 1)));

/** An arrow key moves by one stop inside the allowed range. */
export const stepStop = (i: number, dir: 1 | -1, n: number, firstOk: number): number =>
  Math.max(firstOk, Math.min(n - 1, i + dir));

/** The position of stop `i` of `n` along the track, in percent. */
export const stopPos = (i: number, n: number): number => (n <= 1 ? 0 : (i / (n - 1)) * 100);

export const gbLabel = (v: number): string => (v >= 1000 ? `${v / 1000} ТБ` : `${v} ГБ`);
export const stopLabel = (v: number, unit: 'cores' | 'gb'): string =>
  unit === 'cores' ? String(v) : v >= 1000 ? `${v / 1000} ТБ` : String(v);

/** Roubles from an amount in kopecks; null for another currency. */
export const rubOf = (m: Money | null | undefined): number | null =>
  m && m.currency.toUpperCase() === 'RUB' ? Math.round(m.amount / 100) : null;

export const rubText = (n: number): string => `${Math.round(Math.abs(n)).toLocaleString('ru-RU')} ₽`;

/** «+1 200 ₽» or «−450 ₽», with its direction; null for no change. */
export const deltaOf = (m: Money | null | undefined): { text: string; dir: 'up' | 'down' } | null => {
  const r = rubOf(m);
  if (r == null || r === 0) return null;
  return { text: `${r > 0 ? '+' : '−'}${rubText(r)}`, dir: r > 0 ? 'up' : 'down' };
};

export const sameSize = (
  a: { cores: number; ramGb: number; diskGb: number },
  b: { cores: number; ramGb: number; diskGb: number },
): boolean => a.cores === b.cores && a.ramGb === b.ramGb && a.diskGb === b.diskGb;

export const sizeText = (s: { cores: number; ramGb: number; diskGb: number }): string =>
  `${s.cores} ${plural(s.cores, 'ядро', 'ядра', 'ядер')} · ${s.ramGb} ГБ · ${gbLabel(s.diskGb)}`;

// ── Access ──────────────────────────────────────────────────────────────────────────────────────────────────────────

export const peopleWord = (n: number): string => `${n} ${plural(n, 'человек', 'человека', 'человек')}`;

// ── The status line of the card ─────────────────────────────────────────────────────────────────────────────────────

export type StatusSt = RowSt | 'busy' | 'disabled';

export interface Status {
  st: StatusSt;
  label: string;
  hint: string;
}

/** «только что» or «12 мин назад», then who switched it off. */
const disabledWhen = (m: OrgMachine, now: number): string => {
  const t = m.disabledAt ? ago(m.disabledAt, now) : '';
  const when = !t ? '' : t === '1 мин' ? 'только что' : `${t} назад`;
  return [when, m.disabledBy ?? ''].filter(Boolean).join(', ');
};

/** What the card's status line says: a running action first, then the state; `done` is the hint after an action. */
export const statusOf = (
  m: OrgMachine,
  busy: MachineBusyKind | null,
  done: string | null,
  now: number,
): Status => {
  if (busy) {
    const [label, hint] = BUSY_TEXT[busy](m.busy?.chats ?? m.chats ?? 0);
    return { st: 'busy', label, hint };
  }
  if (m.state === 'disabled') {
    const when = disabledWhen(m, now);
    return {
      st: 'disabled',
      label: 'Отключена',
      hint: `${when ? `${when} · ` : ''}чаты на ней не запустить`,
    };
  }
  if (m.state === 'offline') {
    const t = ago(m.since, now);
    return {
      st: 'off',
      label: 'Не на связи',
      hint: `пропала${t ? ` ${t} назад` : ''} — выключена или нет сети`,
    };
  }
  if (m.state === 'idle') {
    const t = ago(m.since, now);
    return { st: 'idle', label: 'Простаивает', hint: `на связи, но чатов не было${t ? ` ${t}` : ''}` };
  }
  return { st: 'on', label: 'На связи', hint: done ?? 'раннер отвечает' };
};

/** The card's subtitle: «Продукт · общая, сборки и e2e» or «Продукт · личная, Аня». */
export const cardSub = (m: OrgMachine): string =>
  `${m.tenantName} · ${m.kind === 'shared' ? 'общая' : 'личная'}${whoOf(m) ? `, ${whoOf(m)}` : ''}`;

// ── Optimistic changes of the loaded list ───────────────────────────────────────────────────────────────────────────

const bucket = (m: OrgMachine): 'online' | 'idle' | 'offline' => {
  const st = rowSt(m);
  return st === 'on' ? 'online' : st === 'idle' ? 'idle' : 'offline';
};

/** The list with some machines replaced by newer versions of themselves; the counters follow their new states. */
export const applyOverlay = (
  list: MachinesList,
  over: Readonly<Record<string, OrgMachine>>,
): MachinesList => {
  const ids = Object.keys(over);
  if (ids.length === 0) return list;
  const summary = { ...list.summary };
  const groups = list.groups.map((g) => {
    const delta = { online: 0, idle: 0, offline: 0 };
    const machines = g.machines.map((m) => {
      const next = over[m.id];
      if (!next) return m;
      delta[bucket(m)] -= 1;
      delta[bucket(next)] += 1;
      return next;
    });
    if (delta.online === 0 && delta.idle === 0 && delta.offline === 0) {
      return machines.some((m, i) => m !== g.machines[i]) ? { ...g, machines } : g;
    }
    summary.online += delta.online;
    summary.offline += delta.offline;
    return {
      ...g,
      machines,
      online: g.online + delta.online,
      idle: g.idle + delta.idle,
      offline: g.offline + delta.offline,
    };
  });
  return { ...list, summary, groups };
};

// ── The dialogs of the card ─────────────────────────────────────────────────────────────────────────────────────────

export interface OffItem {
  icon: string;
  text: string;
}

/** What switching a machine off does, one line each; a rented server also stops costing for cores and memory. */
export const offItems = (m: OrgMachine, plan: ResourcePlan | null): OffItem[] => {
  const disk = plan && m.hw.diskGb > 0 ? Math.round(plan.price.diskGb * m.hw.diskGb) : null;
  const items: OffItem[] = [
    { icon: 'circle-stop', text: 'Раннер остановится. Если на машине идут чаты, они прервутся.' },
    { icon: 'users', text: 'Люди с доступом не смогут запускать на ней чаты.' },
    { icon: 'hard-drive', text: 'Файлы и репозитории на диске останутся — после подключения всё на месте.' },
  ];
  if (m.cloud) {
    items.push({
      icon: 'coins',
      text: `Сервер выключим: за ядра и память платить перестанем, за диск — ${disk != null && disk > 0 ? `${rubText(disk)} в месяц` : 'по тарифу'}.`,
    });
  }
  return items;
};

/** The hint under «Раннер» / «Всю машину» of the restart dialog. */
export const restartHint = (what: RestartWhat, m: OrgMachine): string =>
  what === 'runner'
    ? 'Перезапустится только раннер harnessly — займёт около 30 секунд. Помогает, когда чаты зависают или не стартуют.'
    : `Сервер перезагрузится целиком — около 2 минут.${m.cloud ? ` Если машина не отвечает, перезагрузим её через ${m.rent?.provider ?? 'провайдера'}.` : ''}`;

/** A machine's name as the hub takes it: spaces collapsed, 1 to 60 characters; null when it does not fit. */
export const cleanName = (raw: string): string | null => {
  const name = raw.trim().replace(/\s+/g, ' ');
  return name.length >= 1 && name.length <= 60 ? name : null;
};

/** Whether the machine's workspace can be opened now: it is on and reachable. */
export const canOpen = (m: OrgMachine): boolean => m.state === 'online' || m.state === 'idle';
