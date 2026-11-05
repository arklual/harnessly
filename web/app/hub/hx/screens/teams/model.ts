// The numbers and texts of the «Команды» and «Команда» screens, computed from the teams view as the board computes them
// (shares, tones, tiles, the limit slider) and the optimistic patches an action applies before the hub answers.
// Everything here is pure: the screens only draw what these functions return.
import type { MemberRole, Person, TeamBlock, TeamsView } from '../../../org/api/teams';
import {
  kRub,
  people as peopleText,
  plural,
  sharePct,
  stopOf,
  thousand,
} from '../../../org/screens/teams/format';
import { inRubles, shownSymbol } from '../../../org/money';
import { initial } from '../../ui';

/** The colours of the teams in the order the board gives them; «без команды» is grey. */
export const PALETTE = ['#8968f4', '#25a897', '#c8830b', '#3b82f6', '#d9534a', '#22a06b'];
export const NO_TEAM_COLOR = '#7c7c86';

export const colorOf = (blocks: TeamBlock[], teamId: string | null): string => {
  if (teamId === null) return NO_TEAM_COLOR;
  const i = blocks.filter((b) => b.id !== null).findIndex((b) => b.id === teamId);
  return PALETTE[(i < 0 ? 0 : i) % PALETTE.length] ?? PALETTE[0]!;
};

export type Tone = 'none' | 'ok' | 'warn' | 'risk';
/** From 75 % of the limit a group warns, from 90 % it is red; without a limit there is nothing to measure. */
export const toneOf = (spent: number, limit: number | null): Tone => {
  if (!limit || limit <= 0) return 'none';
  const share = spent / limit;
  return share >= 0.9 ? 'risk' : share >= 0.75 ? 'warn' : 'ok';
};

const DAY = 86_400_000;
/** From this many days without work a person is «idle» (the row is dimmed and the sub-line is amber). */
const IDLE_DAYS = 7;

export const seenOf = (p: Person, now: number): { text: string; idle: boolean } => {
  if (p.invited) return { text: 'приглашение отправлено', idle: true };
  if (!p.lastActiveAt) return { text: 'ещё не работал(а)', idle: true };
  const ms = Math.max(0, now - Date.parse(p.lastActiveAt));
  const min = Math.floor(ms / 60_000);
  if (min < 2) return { text: 'работает сейчас', idle: false };
  if (min < 60) return { text: `${min} мин назад`, idle: false };
  const h = Math.floor(min / 60);
  if (h < 24) return { text: `${h} ч назад`, idle: false };
  const days = Math.floor(ms / DAY);
  if (days < 2) return { text: 'вчера', idle: false };
  return { text: `${days} ${plural(days, 'день', 'дня', 'дней')} назад`, idle: days >= IDLE_DAYS };
};

export const roleWord = (r: MemberRole): string => (r === 'lead' ? 'лид' : 'участник');

export interface PersonRow {
  id: string;
  ini: string;
  name: string;
  sub: string;
  idle: boolean;
  lead: boolean;
  role: string;
  hot: boolean;
  /** The bar width as a CSS percentage. */
  w: string;
  limTxt: string;
  /** Why the line reads as it does, when it is not the person's own limit. */
  limHint?: string;
  chats: string;
  mr: string;
}

export const rowOf = (p: Person, now: number, team: string | null): PersonRow => {
  // Only the tenant's ceiling acts on the person: it is shared by everyone, so «из 400 тыс» would read as theirs.
  const shared = p.effective.source === 'tenant';
  const limit = shared ? null : p.effective.amountRub;
  const share = limit && limit > 0 ? p.spentRub / limit : 0;
  const seen = seenOf(p, now);
  const spent = p.invited
    ? '—'
    : shared
      ? `${kRub(p.spentRub)} · без своего лимита`
      : limit === null
        ? kRub(p.spentRub)
        : `${thousand(p.spentRub)} из ${kRub(limit)}`;
  return {
    id: p.id,
    ini: initial(p.name),
    name: p.name,
    sub: team ? `${team} · ${seen.text}` : seen.text,
    idle: seen.idle,
    lead: p.role === 'lead',
    role: roleWord(p.role),
    hot: share >= 0.8,
    w: `${Math.min(100, Math.round(share * 100))}%`,
    limTxt: p.invited ? (limit === null ? '—' : `— из ${kRub(limit)}`) : spent,
    limHint: shared
      ? `Своего лимита нет: действует общий лимит тенанта ${kRub(p.effective.amountRub ?? 0)} на всех`
      : undefined,
    chats: p.invited || p.chatsWeek === null ? '—' : String(p.chatsWeek),
    mr: p.invited || p.mrsWeek === null ? '—' : String(p.mrsWeek),
  };
};

export interface Group {
  key: string;
  teamId: string | null;
  name: string;
  initial: string;
  color: string;
  sub: string;
  tone: Tone;
  w: string;
  hasFlag: boolean;
  flag: string;
  sum: string;
  /** The gear that opens the team's limit: only a team with a limit of its own. */
  hasLimit: boolean;
  people: Person[];
}

export const groupOf = (b: TeamBlock, blocks: TeamBlock[]): Group => {
  const share = b.limitRub && b.limitRub > 0 ? b.spentRub / b.limitRub : 0;
  const n = b.people.length;
  return {
    key: b.id ?? 'none',
    teamId: b.id,
    name: b.name,
    initial: initial(b.name),
    color: colorOf(blocks, b.id),
    sub:
      b.id === null
        ? `${peopleText(n)} · лимиты из тенанта`
        : b.leadName
          ? `лид ${b.leadName} · ${peopleText(n)}`
          : peopleText(n),
    tone: toneOf(b.spentRub, b.limitRub),
    w: b.limitRub ? `${Math.min(100, Math.round(share * 100))}%` : '100%',
    hasFlag: !!b.limitRub && share >= 0.75,
    flag: `${Math.round(share * 100)}% лимита`,
    sum: b.limitRub ? `${thousand(b.spentRub)} из ${kRub(b.limitRub)}` : kRub(b.spentRub),
    hasLimit: b.id !== null && b.limitRub !== null,
    people: b.people,
  };
};

/** The search of the board: a person's name or the name of their team; a team that matches shows everybody. */
export const matches = (q: string, p: Person, team: string): boolean => {
  const n = q.trim().toLowerCase();
  return !n || p.name.toLowerCase().includes(n) || team.toLowerCase().includes(n);
};

export const groupsOf = (view: TeamsView, q: string): Group[] => {
  const needle = q.trim();
  return view.teams
    .map((b) => groupOf({ ...b, people: b.people.filter((p) => matches(needle, p, b.name)) }, view.teams))
    .filter((g) => !needle || g.people.length > 0);
};

export const flatOf = (view: TeamsView, q: string): { person: Person; team: string }[] =>
  view.teams.flatMap((b) =>
    b.people.filter((p) => matches(q, p, b.name)).map((person) => ({ person, team: b.name })),
  );

// ───────────────────────── tiles ─────────────────────────

export interface Tiles {
  spend: { parts: { weight: number; color?: string }[]; value: string };
  active: { bars: number[]; value: string };
  near: { parts: { weight: number; color?: string }[]; value: string };
  idle: { parts: { weight: number; color?: string }[]; value: string };
}

/** 312 000 -> «312», 17 500 -> «17,5»: the number of a «… тыс ₽» line whose suffix stands once at the end. */
export const kNum = (rub: number): string => thousand(rub).replace(/ тыс$/, '');

export const tilesOf = (v: TeamsView, lead: boolean): Tiles => {
  const all = v.teams.flatMap((t) => t.people);
  const withLimit = all.filter((p) => p.effective.amountRub);
  const share = (p: Person) => sharePct(p.spentRub, p.effective.amountRub) ?? 0;
  const risk = withLimit.filter((p) => share(p) >= 90);
  const warn = withLimit.filter((p) => share(p) >= 80 && share(p) < 90);
  const hot = v.teams
    .map((t) => ({ t, pct: sharePct(t.spentRub, t.limitRub) }))
    .filter((x): x is { t: TeamBlock; pct: number } => x.pct !== null && x.t.id !== null)
    .sort((a, b) => b.pct - a.pct)[0];
  const nearN = risk.length + warn.length;
  const nearLine = [
    hot && hot.pct >= 75 ? `${hot.t.name} — ${hot.pct}%` : null,
    nearN > 0 ? `${peopleText(nearN)} за 80%` : null,
  ].filter(Boolean);
  const limit = v.scope.limitRub;
  const total = Math.max(all.length, v.total);
  const off = v.notAccepted + v.notVisiting;
  return {
    spend: {
      parts:
        limit === null
          ? [{ weight: 1, color: '#8968f4' }]
          : [
              { weight: Math.min(v.scope.spentRub, limit), color: '#8968f4' },
              { weight: Math.max(0, limit - v.scope.spentRub) },
            ],
      value:
        limit === null
          ? `${kRub(v.scope.spentRub)}${v.spendKnown ? '' : ' · учтено только по лимитам'}`
          : `${kNum(v.scope.spentRub)} из ${kRub(limit)} — лимит ${lead ? 'команды' : 'тенанта'}`,
    },
    active: {
      bars: v.activeSeries,
      value: `${v.activeWeek} из ${v.total} ${plural(v.total, 'человека', 'человек', 'человек')} на этой неделе`,
    },
    near: {
      parts: [
        { weight: risk.length, color: '#d9534a' },
        { weight: warn.length, color: '#c8830b' },
        { weight: Math.max(0, total - nearN) },
      ],
      value: nearLine.length ? nearLine.join(' · ') : 'никто',
    },
    idle: {
      parts: [
        { weight: v.notAccepted, color: '#7c7c86' },
        { weight: v.notVisiting, color: '#c8830b' },
        { weight: Math.max(0, total - off) },
      ],
      value: off
        ? [
            v.notAccepted ? `${v.notAccepted} не приняли приглашение` : null,
            v.notVisiting ? `${v.notVisiting} не заходят неделю` : null,
          ]
            .filter(Boolean)
            .join(' · ')
        : 'все на связи',
    },
  };
};

/** «Руководитель — Аня · работает в тенантах Продукт и Платформа», as far as the hub knows it. */
export const teamLead = (b: TeamBlock): string => {
  const tenants = b.extras?.tenants ?? [];
  const where =
    tenants.length === 0
      ? null
      : tenants.length === 1
        ? `работает в тенанте ${tenants[0]!.name}`
        : `работает в тенантах ${tenants
            .slice(0, -1)
            .map((t) => t.name)
            .join(', ')} и ${tenants[tenants.length - 1]!.name}`;
  return [b.leadName ? `Руководитель — ${b.leadName}` : 'Руководителя нет', where]
    .filter(Boolean)
    .join(' · ');
};

// ───────────────────────── the limit slider ─────────────────────────

export const PERSON_STEPS = [5_000, 10_000, 15_000, 20_000, 30_000, 50_000, 80_000];
export const TENANT_STEPS = [100_000, 200_000, 300_000, 400_000, 500_000, 750_000, 1_000_000, 1_500_000];
export const TEAM_STEPS = [50_000, 70_000, 90_000, 110_000, 120_000, 150_000, 200_000, 300_000];

/** The preset stops in the shown currency plus the values that are already set (they may be off the presets). */
export const stopsOf = (presets: number[], ...extra: (number | null)[]): number[] => {
  const set = new Set(presets.map(stopOf));
  for (const e of extra) if (e !== null && e > 0) set.add(e);
  return [...set].sort((a, b) => a - b);
};

export interface Slider {
  steps: number[];
  /** Index of the lowest stop that can still be picked (what is spent is the floor), and of the highest. */
  lo: number;
  hi: number;
  sel: number;
  fill: string;
  lock: string;
  opts: {
    value: number;
    x: string;
    label: string;
    on: boolean;
    done: boolean;
    cur: boolean;
    off: boolean;
    why: string;
  }[];
}

export const sliderOf = (
  steps: number[],
  draft: number | null,
  base: number | null,
  floorRub: number,
  ceilingRub: number | null,
): Slider => {
  const last = steps.length - 1;
  const firstOk = steps.findIndex((v) => v >= floorRub);
  const lo = firstOk < 0 ? last : firstOk;
  let hi = last;
  if (ceilingRub !== null) {
    hi = steps.reduce((m, v, i) => (v <= ceilingRub ? i : m), -1);
    // The value that is set now is never locked, whatever the room left.
    const cur = base === null ? -1 : steps.indexOf(base);
    hi = Math.max(hi, cur, lo);
  }
  const at = (i: number) => (last <= 0 ? '0%' : `${((i / last) * 100).toFixed(2)}%`);
  const sel = draft === null ? -1 : steps.indexOf(draft);
  return {
    steps,
    lo,
    hi,
    sel,
    fill: at(sel < 0 ? lo : sel),
    lock: lo > 0 ? at(lo) : '0%',
    opts: steps.map((value, i) => ({
      value,
      x: at(i),
      label: kNum(value),
      on: i === sel,
      done: sel >= 0 && i < sel && i >= lo,
      cur: value === base,
      off: i < lo || i > hi,
      why: i < lo ? 'Уже потрачено больше' : i > hi ? 'У тенанта нет столько свободных денег' : '',
    })),
  };
};

/** The stop nearest to a position on the track (0..1), kept inside what can be picked. */
export const snapStop = (s: Slider, fraction: number): number => {
  const last = s.steps.length - 1;
  const i = Math.round(Math.min(1, Math.max(0, fraction)) * last);
  return s.steps[Math.min(s.hi, Math.max(s.lo, i))]!;
};

export const stepStop = (s: Slider, dir: 1 | -1): number => {
  const from = s.sel < 0 ? s.lo : s.sel;
  return s.steps[Math.min(s.hi, Math.max(s.lo, from + dir))]!;
};

export const sliderUnit = (): string => (inRubles() ? `тыс ${shownSymbol()}` : shownSymbol());

/** What a team may be given: the tenant's limit less what the other teams already hold. */
export const teamCeiling = (v: TeamsView, b: TeamBlock): number | null =>
  v.scope.limitRub === null ? null : Math.max(0, v.scope.limitRub - b.siblingsLimitRub);

export const teamLimitNote = (v: TeamsView, b: TeamBlock, draft: number | null): string => {
  const limit = v.scope.limitRub;
  if (limit === null || draft === null) return '';
  return `Из лимита тенанта ${kRub(limit)} другим командам отдано ${thousand(b.siblingsLimitRub)} — свободно ${kRub(limit - b.siblingsLimitRub - draft)}.`;
};

export const ROLE_HINT: Record<MemberRole, string> = {
  lead: 'Лид видит аналитику и лимиты своей команды и может менять лимиты людей в ней',
  member: 'Запускает чаты на машинах, к которым у него есть доступ',
};

// ───────────────────────── the team's page ─────────────────────────

export interface Budget {
  value: string;
  hint: string;
  /** 0..1 of the team's limit that is spent. */
  w: number;
  warn: boolean;
}

export const budgetOf = (b: TeamBlock): Budget => {
  const x = b.extras;
  const free =
    x?.tenantLimitRub != null && x.tenantSpentRub != null
      ? Math.max(0, x.tenantLimitRub - x.tenantSpentRub)
      : null;
  return {
    value: b.limitRub === null ? 'без лимита' : kRub(b.limitRub),
    hint: `Потрачено ${kRub(b.spentRub)}${free === null ? '' : ` · у тенанта свободно ещё ${kRub(free)}`}`,
    w: b.limitRub ? Math.min(1, b.spentRub / b.limitRub) : 0,
    warn: !!b.limitRub && b.spentRub / b.limitRub > 0.85,
  };
};

const machinesWord = (n: number) => `${n} ${plural(n, 'машина', 'машины', 'машин')}`;

export const machinesHint = (b: TeamBlock): string => {
  const x = b.extras;
  if (!x || x.machinesNow == null) return 'Сколько машин может быть у каждого человека команды';
  return `Сейчас у команды ${machinesWord(x.machinesNow)}${x.machinesFree != null ? ` · у тенанта свободно ${x.machinesFree}` : ''}`;
};

export const personMeta = (p: Person, now: number): string => {
  const seen = seenOf(p, now);
  if (p.invited) return 'приглашение отправлено';
  return `${p.machines == null ? 'машин —' : machinesWord(p.machines)} · ${seen.text}`;
};

export const envSummary = (b: TeamBlock): string => {
  const e = b.extras?.env;
  if (!e) return 'Скиллы, MCP, репозитории и документы — из организации, тенанта и команды';
  return `Скиллы: ${e.skills} · MCP: ${e.mcp} · репы: ${e.repos} · доки: ${e.docs} — из организации, тенанта и команды`;
};

export const secretsText = (n: number | null | undefined): string =>
  n == null ? '—' : `${n} ${plural(n, 'секрет', 'секрета', 'секретов')}`;

// ───────────────────────── optimistic patches ─────────────────────────

/** The limit that acts on a person: the tightest of the personal, the team's and the tenant's. */
export const effectiveOf = (
  personal: number | null,
  team: number | null,
  tenant: number | null,
  keep: Person['effective'],
): Person['effective'] => {
  const c: { amountRub: number; source: 'personal' | 'team' | 'tenant' }[] = [];
  if (personal !== null) c.push({ amountRub: personal, source: 'personal' });
  if (team !== null) c.push({ amountRub: team, source: 'team' });
  if (tenant !== null) c.push({ amountRub: tenant, source: 'tenant' });
  const best = c.sort((a, b) => a.amountRub - b.amountRub)[0];
  if (!best) return { amountRub: null, source: 'none', setBy: null, setAt: null };
  return {
    amountRub: best.amountRub,
    source: best.source,
    setBy: keep.source === best.source ? keep.setBy : null,
    setAt: keep.source === best.source ? keep.setAt : null,
  };
};

const handedOut = (b: TeamBlock): number | null =>
  b.limitRub ? Math.round((b.people.reduce((n, p) => n + (p.limitRub ?? 0), 0) / b.limitRub) * 100) : null;

/** Recomputes what depends on the people and limits of a block: who is bound by which limit, how much is handed out. */
const settle = (v: TeamsView): TeamsView => {
  const teamSum = v.teams.reduce((n, t) => n + (t.id !== null ? (t.limitRub ?? 0) : 0), 0);
  const teams = v.teams.map((t) => {
    const withEff = {
      ...t,
      siblingsLimitRub: t.id === null ? 0 : teamSum - (t.limitRub ?? 0),
      people: t.people.map((p) => ({
        ...p,
        effective: effectiveOf(p.limitRub, t.limitRub, null, p.effective),
      })),
    };
    return { ...withEff, handedOutPct: handedOut(withEff) };
  });
  return { ...v, teams };
};

const mapPerson = (v: TeamsView, id: string, fn: (p: Person) => Person): TeamsView => ({
  ...v,
  teams: v.teams.map((t) => ({ ...t, people: t.people.map((p) => (p.id === id ? fn(p) : p)) })),
});

export const patchPersonLimit = (v: TeamsView, id: string, rub: number | null): TeamsView =>
  settle(mapPerson(v, id, (p) => ({ ...p, limitRub: rub, limitSetBy: null, limitSetAt: null })));

export const patchRole = (v: TeamsView, id: string, role: MemberRole): TeamsView => {
  const next = mapPerson(v, id, (p) => ({ ...p, role }));
  const team = next.teams.find((t) => t.people.some((p) => p.id === id));
  if (!team) return next;
  const name = role === 'lead' ? (team.people.find((p) => p.id === id)?.name ?? null) : team.leadName;
  return {
    ...next,
    teams: next.teams.map((t) =>
      t === team ? { ...t, leadName: role === 'lead' ? name : t.leadName === name ? null : t.leadName } : t,
    ),
  };
};

export const patchTeamLimit = (v: TeamsView, teamId: string, rub: number): TeamsView =>
  settle({
    ...v,
    teams: v.teams.map((t) => (t.id === teamId ? { ...t, limitRub: rub, limitSetBy: null } : t)),
  });

export const patchMove = (v: TeamsView, id: string, teamId: string | null): TeamsView => {
  const from = v.teams.find((t) => t.people.some((p) => p.id === id));
  const person = from?.people.find((p) => p.id === id);
  if (!from || !person || from.id === teamId) return v;
  const moved: Person = { ...person, teamId, role: teamId === null ? 'member' : person.role };
  const hasTarget = v.teams.some((t) => t.id === teamId);
  const teams = v.teams.map((t) => {
    if (t === from)
      return { ...t, spentRub: t.spentRub - person.spentRub, people: t.people.filter((p) => p.id !== id) };
    if (t.id === teamId)
      return { ...t, spentRub: t.spentRub + person.spentRub, people: [...t.people, moved] };
    return t;
  });
  if (!hasTarget) {
    teams.push({
      id: null,
      name: 'Без команды',
      leadName: null,
      spentRub: person.spentRub,
      limitRub: null,
      limitSetBy: null,
      handedOutPct: null,
      siblingsLimitRub: 0,
      can: { limit: false },
      needs: { limit: null },
      people: [moved],
      rules: [],
    });
  }
  return settle({ ...v, teams });
};

export const patchRemove = (v: TeamsView, id: string): TeamsView => {
  const from = v.teams.find((t) => t.people.some((p) => p.id === id));
  const person = from?.people.find((p) => p.id === id);
  if (!from || !person) return v;
  return settle({
    ...v,
    total: Math.max(0, v.total - 1),
    notAccepted: person.invited ? Math.max(0, v.notAccepted - 1) : v.notAccepted,
    teams: v.teams.map((t) =>
      t === from
        ? { ...t, spentRub: t.spentRub - person.spentRub, people: t.people.filter((p) => p.id !== id) }
        : t,
    ),
  });
};

export const patchNewTeam = (v: TeamsView, name: string): TeamsView => {
  const block: TeamBlock = {
    id: `new:${name}`,
    name: name.trim(),
    leadName: null,
    spentRub: 0,
    limitRub: null,
    limitSetBy: null,
    handedOutPct: null,
    siblingsLimitRub: 0,
    can: { limit: true },
    needs: { limit: null },
    people: [],
    rules: [],
  };
  const at = v.teams.findIndex((t) => t.id === null);
  const teams = at < 0 ? [...v.teams, block] : [...v.teams.slice(0, at), block, ...v.teams.slice(at)];
  return settle({ ...v, teams });
};

export const patchMachines = (v: TeamsView, teamId: string, n: number): TeamsView => ({
  ...v,
  teams: v.teams.map((t) => (t.id === teamId ? { ...t, extras: { ...t.extras, machinesPerPerson: n } } : t)),
});

export const patchRename = (v: TeamsView, teamId: string, name: string): TeamsView => ({
  ...v,
  teams: v.teams.map((t) => (t.id === teamId ? { ...t, name: name.trim() } : t)),
});
