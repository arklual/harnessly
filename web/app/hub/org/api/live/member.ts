// The participant's own view on /api/me/* (docs/hub/analytics.md, limits.md). The hub keeps no text of a chat, so a
// chat is named by its model.
import type { MeSession, MeSessionsResponse, MeUsageResponse } from '../../../../hub-api/analytics';
import type { LimitRule, MeLimitsResponse } from '../../../../hub-api/limits';
import { setShownCurrency, spentOf } from '../../money';
import type { MemberApi, MyChat, MyLimit, MyOverview } from '../member';
import { OrgApiError } from '../errors';
import { http } from './http';
import { shownAmount } from './teams';

const DAY = 86_400_000;
const WEEK = 7 * DAY;
const BARS = 13;
const CHATS = 8;
const PAGES = 5;

const dayKey = (ms: number): string => new Date(ms).toISOString().slice(0, 10);

/** The rule that stands for «my limit»: the money rule of the month closest to its end, of equals the smallest. */
export const bindingMoneyRule = (limits: MeLimitsResponse): LimitRule | null => {
  const money = limits.rules.filter(
    (r) => r.unit === 'money' && r.period === 'month' && !r.forbid && !r.accountId && !r.modelPattern,
  );
  return [...money].sort((a, b) => b.pct - a.pct || a.amount - b.amount)[0] ?? null;
};

/** Where the month ends at the pace so far. */
export const forecastOf = (spent: number, now: number): number => {
  const d = new Date(now);
  const start = Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1);
  const end = Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 1);
  return spent / Math.max((now - start) / (end - start), 1 / 31);
};

/** The machine of the newest chat that has one; «online» when a chat runs on it now. */
const latestMachine = (live: MeSession[]): MyOverview['tiles']['machine'] => {
  const last = live.find((s) => s.machine);
  if (!last?.machine) return null;
  const m = last.machine;
  return {
    id: m.id ?? null,
    name: m.name,
    online: live.some((s) => s.endedAt === null && s.machine?.id === m.id),
    loadPct: null,
    bars: [],
  };
};

const chatOf = (s: MeSession): MyChat => {
  const running = s.endedAt === null;
  const hasMr = s.mrs > 0;
  const end: MyChat['end'] = running ? 'running' : hasMr ? 'review' : 'abandoned';
  return {
    id: s.sessionKey,
    title: s.models[0] ?? s.provider,
    machine: s.machine?.name ?? '—',
    machineId: s.machine?.id ?? null,
    end,
    mr: hasMr ? String(s.mrs) : null,
    endedAt: s.endedAt,
    costUsd: s.list.usd,
  };
};

const barsBy = (sessions: MeSession[], now: number, value: (s: MeSession) => number): number[] =>
  Array.from({ length: BARS }, (_, i) => {
    const day = dayKey(now - (BARS - 1 - i) * DAY);
    return sessions.filter((s) => s.startedAt.slice(0, 10) === day).reduce((n, s) => n + value(s), 0);
  });

export interface MemberInput {
  limits: MeLimitsResponse;
  usage: MeUsageResponse;
  sessions: MeSession[];
  now: number;
}

export const buildMyOverview = ({ limits, usage, sessions, now }: MemberInput): MyOverview => {
  const shown = (usd: number) => shownAmount(usd, limits.currency);
  const breakdown = usage.byModel.map((m) => ({ label: m.model, kind: 'model' as const, rub: m.list.shown }));
  const rule = bindingMoneyRule(limits);
  const limit: MyLimit | null = rule && {
    spentRub: shown(rule.used),
    limitRub: shown(rule.amount),
    forecastRub: forecastOf(shown(rule.used), now),
    warnPct: Math.round(rule.warnPct * 100),
    mode: limits.mode,
    setBy: rule.setBy ?? { userId: '', name: 'руководитель' },
    resetsAt: rule.resetsAt ?? new Date(now).toISOString(),
    breakdown,
  };
  const live = sessions.filter((s) => s.origin === 'live');
  const week = live.filter((s) => now - Date.parse(s.startedAt) < WEEK);
  return {
    limit,
    spentRub: spentOf(usage.total).shown,
    estimated: spentOf(usage.total).estimated,
    breakdown,
    chats: live.slice(0, CHATS).map(chatOf),
    tiles: {
      chats: {
        week: week.length,
        running: live.filter((s) => s.endedAt === null).length,
        bars: barsBy(live, now, () => 1),
      },
      // The hub counts a session's MRs; which of them are merged it tells only in KPI.
      mr: { week: week.reduce((n, s) => n + s.mrs, 0), merged: null, bars: barsBy(live, now, (s) => s.mrs) },
      // The machine of the latest chat; the hub reports no load per machine here.
      machine: latestMachine(live),
    },
  };
};

export const allSessions = async (query: Record<string, string>): Promise<MeSession[]> => {
  const out: MeSession[] = [];
  let before: string | undefined;
  for (let page = 0; page < PAGES; page += 1) {
    const r = await http.get<MeSessionsResponse>('/api/me/sessions', { ...query, before, limit: 50 });
    out.push(...r.sessions);
    if (!r.next) break;
    before = r.next;
  }
  return out;
};

export const liveMember = (): MemberApi => {
  let rule: LimitRule | null = null;
  return {
    myOverview: async (period) => {
      const now = Date.now();
      const [limits, usage, sessions] = await Promise.all([
        http.get<MeLimitsResponse>('/api/me/limits'),
        http.get<MeUsageResponse>('/api/me/usage', { period }),
        allSessions({ from: dayKey(now - BARS * DAY), to: dayKey(now + DAY) }),
      ]);
      setShownCurrency(limits.currency.code);
      rule = bindingMoneyRule(limits);
      return buildMyOverview({ limits, usage, sessions, now });
    },
    requestMoreLimit: async () => {
      if (!rule) throw new OrgApiError('invalid_request', 'Лимита нет: просить больше не у кого.');
      await http.post('/api/me/limit-requests', {
        amountUsd: rule.amount,
        reason: 'Просит больше с экрана «Обзор»',
        scope: rule.scope,
      });
      return { sent: true as const };
    },
  };
};
