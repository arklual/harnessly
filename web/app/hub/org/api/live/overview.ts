// Обзор on `GET …/overview` (docs/hub/analytics.md). The amounts come in the organization's currency (`shown`).
import type {
  LimitBar,
  OverviewResponse,
  Signal as ApiSignal,
  SpendResponse,
  SpendRow,
  Subscription,
} from '../../../../hub-api/analytics';
import { forecastShown, setShownCurrency, spentOf } from '../../money';
import type {
  OverviewApi,
  OverviewData,
  PersonSpend,
  Signal,
  SpendDetail,
  SpendLimit,
  SpendLine,
  SubscriptionUse,
  TeamEfficiency,
} from '../overview';
import { contractGet, orgPath } from './http';
import { signalGo } from './signal-go';
import type { AnalyticsCtx } from './analytics';

const signalOf = (s: ApiSignal): Signal => ({
  id: s.id,
  severity: s.severity === 'critical' ? 'critical' : 'warn',
  title: s.title,
  detail: s.detail,
  action: signalGo(s.action),
});

const worstWindow = (s: Subscription): { pct: number; resetsAt: string | null } => {
  const measured = s.windows.filter((w) => w.usedPct !== null);
  const worst = [...measured].sort((a, b) => (b.usedPct ?? 0) - (a.usedPct ?? 0))[0];
  return { pct: worst?.usedPct ?? 0, resetsAt: worst?.resetsAt ?? null };
};

const subscriptionOf = (s: Subscription): SubscriptionUse => {
  const { pct, resetsAt } = worstWindow(s);
  return {
    id: s.accountId,
    name: s.label,
    teams: s.shares.map((x) => x.person.name),
    resetsOn: resetsAt ? new Date(resetsAt).toLocaleDateString('ru-RU', { weekday: 'short' }) : '—',
    usedPct: Math.round(pct),
  };
};

const sumTokens = (t: { input: number; output: number; cacheRead: number; cacheWrite: number }) =>
  t.input + t.output + t.cacheRead + t.cacheWrite;

export const buildOverview = (r: OverviewResponse, teamName: string | null, people: number): OverviewData => {
  const rows = r.spendBy.rows;
  const spent = spentOf(r.spend);
  const total = spentOf(r.spendBy.totals);
  const today = r.today;
  const todaySpent = spentOf(today.spend);
  const subscriptions = r.subscriptions.map(subscriptionOf);
  const worst = [...subscriptions].sort((a, b) => b.usedPct - a.usedPct)[0];
  const bars = (xs: { value: number }[]) => xs.map((x) => x.value);
  const money = (x: (typeof rows)[number]) => ({
    id: x.key,
    name: x.label,
    rub: spentOf(x).shown,
    usd: spentOf(x).usd,
    tokens: sumTokens(x.tokens),
    ...(x.key === '' ? { noTeam: true } : {}),
  });
  const byPerson: PersonSpend[] = rows.map((x) => ({
    userId: x.key,
    name: x.label,
    isLead: false,
    lastActiveAt: null,
    invited: false,
    spentRub: spentOf(x).shown,
    limitRub: x.limit?.amount.shown ?? null,
  }));
  const efficiency: TeamEfficiency[] = r.efficiency.teams.map((t) => ({
    id: t.key,
    name: t.label,
    tasksPerPerson: t.perPersonMonth ?? 0,
    resultPct: t.reached.value === null ? null : Math.round(t.reached.value * 100),
    ...(t.key === '' ? { noTeam: true } : {}),
  }));
  return {
    estimated: spent.estimated,
    tiles: {
      basis: 'today',
      spend: {
        rub: todaySpent.shown,
        usd: todaySpent.usd,
        tokens: today.spend.tokens,
        bars: bars(today.sparks.spend),
      },
      active: {
        now: today.active.people,
        total: today.active.of || people,
        bars: bars(today.sparks.active),
        who: (today.active.who ?? []).map((p) => ({
          userId: p.userId,
          name: p.name || p.email,
          email: p.email,
          working: p.working,
          chats: p.chats,
          closed: p.closed,
          lastAt: p.lastAt,
          rub: p.paid.shown,
          listRub: p.list.shown,
          tokens: p.tokens,
        })),
      },
      tasks: { closed: today.tasks.closed, inProgress: today.tasks.inWork, bars: bars(today.sparks.tasks) },
      subscriptions: {
        accounts: subscriptions.length,
        worstName: worst?.name ?? '',
        worstPct: worst?.usedPct ?? 0,
      },
    },
    signals: r.attention.map(signalOf),
    subscriptions,
    byTeam:
      r.spendBy.by === 'person'
        ? null
        : {
            total: { rub: total.shown, usd: total.usd, tokens: r.spendBy.totals.tokens },
            teams: rows.map(money),
            efficiency,
          },
    byPerson:
      r.spendBy.by === 'person'
        ? {
            teamName: teamName ?? '',
            spentRub: spent.shown,
            limitRub: r.spend.limit?.amount.shown ?? null,
            people: byPerson,
          }
        : null,
  };
};

const limitOf = (l: LimitBar | null): SpendLimit | null =>
  l ? { rub: l.amount.shown, pct: Math.round(l.pct * 100), status: l.status } : null;

const lineOf = (x: SpendRow): SpendLine => {
  const s = spentOf(x);
  return {
    key: x.key,
    label: x.label || (x.key === '' ? 'Без команды' : x.key),
    rub: s.shown,
    usd: s.usd,
    listRub: x.list.shown,
    estimated: s.estimated,
    tokens: x.tokens.total || sumTokens(x.tokens),
    calls: x.calls,
    unpricedCalls: x.unpricedCalls,
    limit: limitOf(x.limit),
    forecastRub: forecastShown(x),
  };
};

export const buildSpendDetail = (r: SpendResponse): SpendDetail => {
  const rows = r.rows.map(lineOf).sort((a, b) => b.rub - a.rub || b.tokens - a.tokens);
  const s = spentOf(r.total);
  return {
    by: r.by === 'tenant' ? 'team' : r.by,
    span: 'month',
    total: {
      rub: s.shown,
      usd: s.usd,
      listRub: r.total.list.shown,
      estimated: s.estimated,
      tokens: rows.reduce((n, x) => n + x.tokens, 0),
      calls: rows.reduce((n, x) => n + x.calls, 0),
    },
    limit: limitOf(r.total.limit),
    forecastRub: forecastShown(r.total),
    rows,
    accountedUntil: r.accountedUntil,
  };
};

export const liveOverview = (
  ctx: () => AnalyticsCtx & { teamName: string | null; people: number },
): OverviewApi => ({
  overview: async (_tenant, period) => {
    const c = ctx();
    const r = await contractGet<OverviewResponse>(orgPath(c.org, '/overview'), {
      scope: `${c.scope.kind}:${c.scope.id}`,
      period,
    });
    setShownCurrency(r.currency.code);
    return buildOverview(r, c.teamName, c.people);
  },
  spendDetail: async (by, span) => {
    const c = ctx();
    const r = await contractGet<SpendResponse>(orgPath(c.org, '/spend'), {
      scope: `${c.scope.kind}:${c.scope.id}`,
      by,
      period: span,
    });
    setShownCurrency(r.currency.code);
    return { ...buildSpendDetail(r), by, span };
  },
});
