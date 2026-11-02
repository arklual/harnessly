// Обзор on `GET …/overview` (docs/hub/analytics.md). The amounts come in the organization's currency (`shown`).
import type { OverviewResponse, Signal as ApiSignal, Subscription } from '../../../../hub-api/analytics';
import { setShownCurrency, spentOf } from '../../money';
import type { Section } from '../../types';
import type { OverviewApi, OverviewData, PersonSpend, Signal, SubscriptionUse } from '../overview';
import { contractGet, orgPath } from './http';
import type { AnalyticsCtx } from './analytics';

const SECTION: Record<NonNullable<ApiSignal['action']>['kind'], Section> = {
  raise_limit: 'teams',
  move_account: 'teams',
  open_machine: 'machines',
  resend_invite: 'teams',
  register_account: 'teams',
  open_kpi: 'analytics',
  open_chat: 'overview',
  open_prices: 'overview',
};

const signalOf = (s: ApiSignal): Signal => ({
  id: s.id,
  severity: s.severity === 'critical' ? 'critical' : 'warn',
  title: s.title,
  detail: s.detail,
  action: s.action
    ? { label: s.action.label, section: SECTION[s.action.kind] }
    : { label: 'Открыть', section: 'overview' },
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

export const buildOverview = (r: OverviewResponse, teamName: string | null, people: number): OverviewData => {
  const rows = r.spendBy.rows;
  const spent = spentOf(r.spend);
  const tokens = rows.reduce(
    (n, x) => n + x.tokens.input + x.tokens.output + x.tokens.cacheRead + x.tokens.cacheWrite,
    0,
  );
  const subscriptions = r.subscriptions.map(subscriptionOf);
  const worst = [...subscriptions].sort((a, b) => b.usedPct - a.usedPct)[0];
  const money = (x: (typeof rows)[number]) => ({
    id: x.key,
    name: x.label,
    rub: spentOf(x).shown,
    usd: spentOf(x).usd,
    tokens: x.tokens.input + x.tokens.output + x.tokens.cacheRead + x.tokens.cacheWrite,
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
  return {
    estimated: spent.estimated,
    tiles: {
      spend: { rub: spent.shown, usd: spent.usd, tokens, bars: [] },
      active: { now: r.tiles.activePeople.value ?? 0, total: people, bars: [] },
      tasks: { closed: r.tiles.mrs.value ?? 0, inProgress: r.tiles.sessions.value ?? 0, bars: [] },
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
            total: { rub: spent.shown, usd: spent.usd, tokens },
            teams: rows.map(money),
            efficiency: [],
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
});
