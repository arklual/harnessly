import { appConfig } from '@/platform/config';
import { t } from '@/lib/i18n';
// Pure calculations behind the statistics tab: merge the per-runtime reports
// into people, cut them by period and filters, and derive every figure the
// charts show. Kept free of React so the numbers can be tested directly.

import type { ITeamUsage, IUsageDay, IUsageSession, TUsageProvider } from '@/lib/hl-usage/types';

export type TPeriod = 7 | 30 | 90 | 365;
export type TProviderFilter = 'all' | TUsageProvider;

const PERSON_NAMES = (): Record<string, string> => appConfig().people;
// Colour follows the person, never their rank: indexes into the chart palette.
const PERSON_ORDER = (): string[] => Object.keys(appConfig().people);

export const personName = (person: string): string => {
  const login = person.split('@')[0];
  return PERSON_NAMES()[login] ?? login;
};

export const personColorIndex = (person: string, fallback: number): number => {
  const order = PERSON_ORDER();
  const at = order.indexOf(person.split('@')[0] ?? '');
  return at === -1 ? order.length + fallback : at;
};

export interface IPersonSession extends IUsageSession {
  person: string;
  machineName: string;
}

export interface IUsageFact {
  day: string;
  person: string;
  provider: TUsageProvider;
  model: string;
  project: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  calls: number;
}

export interface ITeamData {
  viewer: string;
  people: string[];
  days: Map<string, Map<string, IUsageDay>>;
  facts: IUsageFact[];
  sessions: IPersonSession[];
  unavailable: string[];
  collecting: { machine: string; done: number; total: number }[];
  latestDay: string;
}

const addDay = (into: IUsageDay, day: IUsageDay) => {
  into.prompts += day.prompts;
  into.sessions += day.sessions;
  into.turnMs += day.turnMs;
  into.tools += day.tools;
  into.toolErrors += day.toolErrors;
  into.interrupts += day.interrupts;
  into.denials += day.denials;
  into.compactions += day.compactions;
  into.prs += day.prs;
  for (let h = 0; h < 24; h++) into.hours[h] += day.hours[h] ?? 0;
};

export const emptyDay = (): IUsageDay => ({
  prompts: 0,
  sessions: 0,
  turnMs: 0,
  tools: 0,
  toolErrors: 0,
  interrupts: 0,
  denials: 0,
  compactions: 0,
  prs: 0,
  hours: Array(24).fill(0),
});

export const mergeTeam = (team: ITeamUsage): ITeamData => {
  const days = new Map<string, Map<string, IUsageDay>>();
  const facts: IUsageFact[] = [];
  const sessions: IPersonSession[] = [];
  const unavailable: string[] = [];
  const collecting: ITeamData['collecting'] = [];
  const people = new Set<string>();
  let latestDay = '';
  for (const runtime of team.runtimes) {
    const report = runtime.report;
    if (!report) {
      unavailable.push(`${personName(runtime.person)} · ${runtime.machineName}`);
      continue;
    }
    people.add(runtime.person);
    if (report.collecting)
      collecting.push({
        machine: `${personName(runtime.person)} · ${runtime.machineName}`,
        ...report.collecting,
      });
    const perPerson = days.get(runtime.person) ?? new Map<string, IUsageDay>();
    days.set(runtime.person, perPerson);
    for (const [day, value] of Object.entries(report.days)) {
      const into = perPerson.get(day) ?? emptyDay();
      addDay(into, value);
      perPerson.set(day, into);
      if (day > latestDay) latestDay = day;
    }
    for (const [
      day,
      provider,
      model,
      project,
      input,
      output,
      cacheRead,
      cacheWrite,
      cost,
      calls,
    ] of report.usage) {
      facts.push({
        day,
        person: runtime.person,
        provider,
        model,
        project,
        input,
        output,
        cacheRead,
        cacheWrite,
        cost,
        calls,
      });
      if (day > latestDay) latestDay = day;
    }
    for (const session of report.sessions)
      sessions.push({ ...session, person: runtime.person, machineName: runtime.machineName });
  }
  const ordered = [...people].sort(
    (a, b) => personColorIndex(a, 9) - personColorIndex(b, 9) || a.localeCompare(b),
  );
  return { viewer: team.viewer, people: ordered, days, facts, sessions, unavailable, collecting, latestDay };
};

// --- dates (report days are Moscow calendar days) ---

const MSK_OFFSET_MS = 3 * 3600_000;
export const todayKey = (): string => new Date(Date.now() + MSK_OFFSET_MS).toISOString().slice(0, 10);
export const shiftDay = (day: string, delta: number): string => {
  const date = new Date(`${day}T00:00:00Z`);
  date.setUTCDate(date.getUTCDate() + delta);
  return date.toISOString().slice(0, 10);
};
export const dayRange = (end: string, length: number): string[] =>
  Array.from({ length }, (_, i) => shiftDay(end, i - length + 1));

export interface IFilters {
  period: TPeriod;
  people: string[];
  provider: TProviderFilter;
}

export interface IWindow {
  days: string[];
  from: string;
  to: string;
}

export const windowFor = (period: TPeriod, end = todayKey(), offset = 0): IWindow => {
  const to = shiftDay(end, -offset * period);
  const days = dayRange(to, period);
  return { days, from: days[0], to };
};

const inWindow = (day: string, w: IWindow) => day >= w.from && day <= w.to;
const personPicked = (filters: IFilters, person: string) =>
  filters.people.length === 0 || filters.people.includes(person);

// Activity counters (prompts, tools, turns) are not split by provider in the
// report; the provider filter narrows tokens, cost and sessions only.
export const activityByDay = (
  data: ITeamData,
  filters: IFilters,
  w: IWindow,
  person?: string,
): Map<string, IUsageDay> => {
  const out = new Map<string, IUsageDay>(w.days.map((d) => [d, emptyDay()]));
  for (const [who, perDay] of data.days) {
    if (person ? who !== person : !personPicked(filters, who)) continue;
    for (const [day, value] of perDay) {
      const into = out.get(day);
      if (into) addDay(into, value);
    }
  }
  return out;
};

/** First day any picked person has data for; comparisons before it would compare against a void. */
export const historyStart = (data: ITeamData, filters: IFilters): string | null => {
  let first: string | null = null;
  for (const [person, perDay] of data.days) {
    if (!personPicked(filters, person)) continue;
    for (const day of perDay.keys()) if (!first || day < first) first = day;
  }
  for (const f of data.facts) if (personPicked(filters, f.person) && (!first || f.day < first)) first = f.day;
  return first;
};

export const factsIn = (data: ITeamData, filters: IFilters, w: IWindow): IUsageFact[] =>
  data.facts.filter(
    (f) =>
      inWindow(f.day, w) &&
      personPicked(filters, f.person) &&
      (filters.provider === 'all' || f.provider === filters.provider),
  );

export const sessionsIn = (data: ITeamData, filters: IFilters, w: IWindow): IPersonSession[] => {
  const from = Date.parse(`${w.from}T00:00:00+03:00`) / 1000;
  const to = Date.parse(`${w.to}T23:59:59+03:00`) / 1000;
  return data.sessions.filter(
    (s) =>
      s.end >= from &&
      s.start <= to &&
      personPicked(filters, s.person) &&
      (filters.provider === 'all' || s.provider === filters.provider),
  );
};

export interface ITotals {
  prompts: number;
  activeDays: number;
  sessions: number;
  turnHours: number;
  tools: number;
  toolErrors: number;
  interrupts: number;
  denials: number;
  prs: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  calls: number;
}

export const totals = (data: ITeamData, filters: IFilters, w: IWindow): ITotals => {
  const byDay = activityByDay(data, filters, w);
  const t: ITotals = {
    prompts: 0,
    activeDays: 0,
    sessions: 0,
    turnHours: 0,
    tools: 0,
    toolErrors: 0,
    interrupts: 0,
    denials: 0,
    prs: 0,
    input: 0,
    output: 0,
    cacheRead: 0,
    cacheWrite: 0,
    cost: 0,
    calls: 0,
  };
  for (const day of byDay.values()) {
    t.prompts += day.prompts;
    if (day.prompts > 0) t.activeDays++;
    t.sessions += day.sessions;
    t.turnHours += day.turnMs / 3_600_000;
    t.tools += day.tools;
    t.toolErrors += day.toolErrors;
    t.interrupts += day.interrupts;
    t.denials += day.denials;
    t.prs += day.prs;
  }
  for (const f of factsIn(data, filters, w)) {
    t.input += f.input;
    t.output += f.output;
    t.cacheRead += f.cacheRead;
    t.cacheWrite += f.cacheWrite;
    t.cost += f.cost;
    t.calls += f.calls;
  }
  return t;
};

/** Share of prompt input served from cache: read / (fresh input + cache write + read). */
export const cacheHitRate = (t: Pick<ITotals, 'input' | 'cacheRead' | 'cacheWrite'>): number | null => {
  const all = t.input + t.cacheRead + t.cacheWrite;
  return all > 0 ? t.cacheRead / all : null;
};

export const ratio = (part: number, whole: number): number | null => (whole > 0 ? part / whole : null);

/** Relative change against the previous period; null when there is nothing to compare with. */
export const change = (current: number, previous: number): number | null =>
  previous > 0 ? (current - previous) / previous : null;

export interface IPersonRow {
  person: string;
  prompts: number;
  activeDays: number;
  turnHours: number;
  sessions: number;
  cost: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  calls: number;
  prs: number;
}

export const leaderboard = (data: ITeamData, filters: IFilters, w: IWindow): IPersonRow[] =>
  data.people
    .filter((p) => personPicked(filters, p))
    .map((person) => {
      const only: IFilters = { ...filters, people: [person] };
      const t = totals(data, only, w);
      return {
        person,
        prompts: t.prompts,
        activeDays: t.activeDays,
        turnHours: t.turnHours,
        sessions: sessionsIn(data, only, w).length,
        cost: t.cost,
        input: t.input,
        output: t.output,
        cacheRead: t.cacheRead,
        cacheWrite: t.cacheWrite,
        calls: t.calls,
        prs: t.prs,
      };
    })
    .sort((a, b) => b.prompts - a.prompts);

export interface ISeries {
  name: string;
  colorIndex: number;
  values: number[];
}

const OTHER = t('stats.aggregate.other');

/** Group a numeric measure by key per day, keeping the top `limit` keys and folding the tail into "Other". */
export const seriesByKey = (
  facts: IUsageFact[],
  w: IWindow,
  key: (f: IUsageFact) => string,
  value: (f: IUsageFact) => number,
  limit = 5,
): ISeries[] => {
  const totalsByKey = new Map<string, number>();
  for (const f of facts) totalsByKey.set(key(f), (totalsByKey.get(key(f)) ?? 0) + value(f));
  const ranked = [...totalsByKey.entries()].filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]);
  const keep = ranked.slice(0, ranked.length > limit ? limit - 1 : limit).map(([k]) => k);
  const index = new Map(w.days.map((d, i) => [d, i]));
  const series = new Map<string, number[]>();
  for (const k of [...keep, ...(ranked.length > keep.length ? [OTHER] : [])])
    series.set(k, Array(w.days.length).fill(0));
  for (const f of facts) {
    const at = index.get(f.day);
    if (at === undefined) continue;
    const name = keep.includes(key(f)) ? key(f) : OTHER;
    const values = series.get(name);
    if (values) values[at] += value(f);
  }
  return [...series.entries()].map(([name, values], i) => ({
    name,
    colorIndex: name === OTHER ? -1 : i,
    values,
  }));
};

export interface IBar {
  name: string;
  value: number;
  extra: Record<string, number>;
}

export const projectBars = (data: ITeamData, filters: IFilters, w: IWindow, limit = 10): IBar[] => {
  const byProject = new Map<string, { cost: number; output: number; calls: number }>();
  for (const f of factsIn(data, filters, w)) {
    const entry = byProject.get(f.project) ?? { cost: 0, output: 0, calls: 0 };
    entry.cost += f.cost;
    entry.output += f.output;
    entry.calls += f.calls;
    byProject.set(f.project, entry);
  }
  const ranked = [...byProject.entries()].sort((a, b) => b[1].cost - a[1].cost);
  const head = ranked.slice(0, limit);
  const tail = ranked.slice(limit);
  const bars = head.map(([name, v]) => ({
    name,
    value: v.cost,
    extra: { output: v.output, calls: v.calls },
  }));
  if (tail.length) {
    const rest = tail.reduce(
      (acc, [, v]) => ({
        cost: acc.cost + v.cost,
        output: acc.output + v.output,
        calls: acc.calls + v.calls,
      }),
      { cost: 0, output: 0, calls: 0 },
    );
    bars.push({
      name: `${OTHER} (${tail.length})`,
      value: rest.cost,
      extra: { output: rest.output, calls: rest.calls },
    });
  }
  return bars;
};

/** Prompts on a weekday × hour grid (Monday first), Moscow time. */
export const punchcard = (data: ITeamData, filters: IFilters, w: IWindow): number[][] => {
  const grid = Array.from({ length: 7 }, () => Array(24).fill(0) as number[]);
  for (const [day, value] of activityByDay(data, filters, w)) {
    const weekday = (new Date(`${day}T00:00:00Z`).getUTCDay() + 6) % 7;
    for (let h = 0; h < 24; h++) grid[weekday][h] += value.hours[h] ?? 0;
  }
  return grid;
};

export interface ITaskRow {
  task: string;
  sessions: number;
  prompts: number;
  turnHours: number;
  cost: number;
  people: string[];
  lastEnd: number;
}

export const taskRows = (sessions: IPersonSession[]): ITaskRow[] => {
  const byTask = new Map<string, ITaskRow>();
  for (const s of sessions) {
    for (const task of s.tasks) {
      const row = byTask.get(task) ?? {
        task,
        sessions: 0,
        prompts: 0,
        turnHours: 0,
        cost: 0,
        people: [],
        lastEnd: 0,
      };
      row.sessions++;
      row.prompts += s.prompts;
      row.turnHours += s.turnMs / 3_600_000;
      // A session touching several tasks is attributed to each; this is a
      // "how much went into" view, not a ledger that must sum to the total.
      row.cost += s.cost;
      if (!row.people.includes(s.person)) row.people.push(s.person);
      row.lastEnd = Math.max(row.lastEnd, s.end);
      byTask.set(task, row);
    }
  }
  return [...byTask.values()].sort((a, b) => b.cost - a.cost);
};
