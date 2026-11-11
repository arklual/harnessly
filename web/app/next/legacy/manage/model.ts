// The numbers of «Управление»: what the hub says merged with what the machine counted. Pure, so tests can reach it.
import {
  factsIn,
  sessionsIn,
  totals,
  windowFor,
  type IFilters,
  type IPersonSession,
  type ITeamData,
  type ITotals,
  type TPeriod,
} from '@/components/agents/stats/aggregate';
import type { MeLimit, MeLimits, MeSession, MeUsage } from '@/next/features/manage/hub-types';

export type LimitTone = 'ok' | 'warn' | 'stop';

/** The limit that decides: the one marked effective, else the tightest by share. Null when nothing limits. */
export const decidingLimit = (limits: MeLimits): MeLimit | null => {
  const marked = limits.limits.find((l) => l.effective);
  if (marked) return marked;
  const sorted = [...limits.limits].sort((a, b) => share(b) - share(a));
  return sorted[0] ?? null;
};

export const share = (l: Pick<MeLimit, 'used' | 'amount'>): number => (l.amount > 0 ? l.used / l.amount : 0);

export const limitTone = (l: MeLimit): LimitTone => {
  const s = share(l);
  if (s >= 1) return 'stop';
  return s * 100 >= l.warnPct ? 'warn' : 'ok';
};

/** Where the spending line crosses the amount: a day of the period, or null when it does not within a month's pace. */
export const runsOutIn = (used: number, amount: number, daysGone: number): number | null => {
  if (used <= 0 || daysGone <= 0 || used >= amount) return null;
  const perDay = used / daysGone;
  return Math.ceil((amount - used) / perDay);
};

export interface LocalSummary {
  period: TPeriod;
  totals: ITotals;
  /** Cost per day of the window, oldest first; days are the window's own, so a quiet day is a 0, not a gap. */
  daily: { day: string; cost: number }[];
  models: { model: string; cost: number }[];
  sessions: IPersonSession[];
}

export const localSummary = (data: ITeamData, period: TPeriod): LocalSummary => {
  const filters: IFilters = { period, people: [data.viewer], provider: 'all' };
  const w = windowFor(period);
  const facts = factsIn(data, filters, w);
  const perDay = new Map<string, number>(w.days.map((d) => [d, 0]));
  const perModel = new Map<string, number>();
  for (const f of facts) {
    perDay.set(f.day, (perDay.get(f.day) ?? 0) + f.cost);
    perModel.set(f.model, (perModel.get(f.model) ?? 0) + f.cost);
  }
  return {
    period,
    totals: totals(data, filters, w),
    daily: w.days.map((day) => ({ day, cost: perDay.get(day) ?? 0 })),
    models: [...perModel.entries()].map(([model, cost]) => ({ model, cost })).sort((a, b) => b.cost - a.cost),
    sessions: sessionsIn(data, filters, w).sort((a, b) => b.end - a.end),
  };
};

/** A local session with what the hub has on the same session (the provider's session id is the key). */
export interface HistoryRow {
  session: IPersonSession;
  hub: MeSession | null;
}

export const historyRows = (sessions: IPersonSession[], hub: MeSession[] | null): HistoryRow[] => {
  const byKey = new Map((hub ?? []).map((s) => [s.key, s]));
  return sessions.map((session) => ({ session, hub: byKey.get(session.id) ?? null }));
};

/** The tiles and the recent list from the hub alone (RUNTIME_HUB_ANALYTICS): no local report behind them. */
export interface HubSummary {
  cost: number;
  sessions: number;
  mrs: number;
  recent: MeSession[];
}

export const hubSummary = (
  me: { usage: MeUsage; sessions: { sessions: MeSession[] } },
  period: number,
): HubSummary => {
  const since = Date.now() - period * 86_400_000;
  const inWindow = me.sessions.sessions
    .filter((s) => s.endedAt === null || Date.parse(s.endedAt) >= since)
    .sort((a, b) => Date.parse(b.endedAt ?? '9999') - Date.parse(a.endedAt ?? '9999'));
  return {
    cost: me.usage.days.slice(-period).reduce((sum, d) => sum + d.cost, 0),
    sessions: inWindow.length,
    mrs: inWindow.filter((s) => s.outcome === 'mr_open').length,
    recent: inWindow,
  };
};
