// Аналитика on `…/kpis`, `…/kpis/{kpi}/series`, `…/changes`, `…/waste`, `…/spend` and, for a participant, `/api/me/*`
// (docs/hub/analytics.md). Whether a difference is noise is the hub's verdict, never worked out here.
import type {
  Change as ApiChange,
  ChangesResponse,
  EffectResponse,
  KpiId as ApiKpi,
  KpiSeriesResponse,
  KpisResponse,
  KpiValue,
  MeUsageResponse,
  OverviewResponse,
  Signal as ApiSignal,
  SignalsResponse,
  WasteResponse,
} from '../../../../hub-api/analytics';
import type { Scope as ApiScope } from '../../../../hub-api/orgs';
import { addDays } from '../../dates';
import type { Role } from '../../types';
import { OrgApiError } from '../errors';
import type {
  AnalyticsApi,
  AnalyticsData,
  Change,
  ChangeKind,
  Effect,
  DeltaKind,
  Kpi,
  KpiChart,
  KpiFormat,
  KpiGroup,
  KpiId,
  Peers,
  Signal,
  Week,
} from '../analytics';
import { formatValue } from '../../screens/analytics/format';
import { spentOf } from '../../money';
import { contractGet, http, orgPath } from './http';
import { allSessions } from './member';

interface KpiMeta {
  id: KpiId;
  group: KpiGroup;
  title: string;
  hint: string;
  format: KpiFormat;
  delta: DeltaKind;
}

/** docs/hub/analytics.md, the table of the twelve. */
export const KPI: Record<ApiKpi, KpiMeta> = {
  money_per_mr: {
    id: 'moneyPerMr',
    group: 'result',
    title: 'Деньги на MR',
    hint: 'Сколько стоил один смёрженный MR.',
    format: 'usd',
    delta: 'rel',
  },
  completion: {
    id: 'yield',
    group: 'result',
    title: 'Доходимость',
    hint: 'Сколько сессий заканчиваются MR.',
    format: 'pct',
    delta: 'pts',
  },
  time_to_mr: {
    id: 'timeToMr',
    group: 'result',
    title: 'Время до MR',
    hint: 'От начала сессии до её первого MR.',
    format: 'minutes',
    delta: 'rel',
  },
  mrs_per_person: {
    id: 'mrPerPerson',
    group: 'result',
    title: 'MR на человека',
    hint: 'Смёрженные MR на активного человека за неделю.',
    format: 'perWeek',
    delta: 'rel',
  },
  autonomy: {
    id: 'autonomy',
    group: 'agent',
    title: 'Автономность',
    hint: 'Какую долю работы агент делает сам.',
    format: 'pct',
    delta: 'pts',
  },
  waiting_human: {
    id: 'waiting',
    group: 'agent',
    title: 'Ждёт человека',
    hint: 'Сколько агент ждёт человека после остановки.',
    format: 'minutes',
    delta: 'rel',
  },
  parallelism: {
    id: 'parallel',
    group: 'agent',
    title: 'Параллельность',
    hint: 'Сколько сессий человек ведёт одновременно.',
    format: 'count',
    delta: 'abs',
  },
  tool_errors: {
    id: 'toolErrors',
    group: 'agent',
    title: 'Ошибки инструментов',
    hint: 'Как часто падает вызов инструмента.',
    format: 'pct',
    delta: 'pts',
  },
  reread_context: {
    id: 'reread',
    group: 'env',
    title: 'Перечитанный контекст',
    hint: 'Доля входа, которая несётся заново.',
    format: 'pct',
    delta: 'pts',
  },
  session_start_ctx: {
    id: 'sessionStart',
    group: 'env',
    title: 'Старт сессии',
    hint: 'С каким контекстом начинается сессия.',
    format: 'tokens',
    delta: 'rel',
  },
  cache_hit: {
    id: 'cache',
    group: 'env',
    title: 'Кэш',
    hint: 'Какая доля входа прочитана из кэша.',
    format: 'pct',
    delta: 'pts',
  },
  skills_in_use: {
    id: 'skills',
    group: 'env',
    title: 'Скиллы в ходу',
    hint: 'Сколько из предложенных скиллов используются.',
    format: 'pct',
    delta: 'pts',
  },
};

const scale = (kpi: ApiKpi, v: number): number => {
  const { format } = KPI[kpi];
  if (format === 'pct') return v * 100;
  if (format === 'minutes') return v / 60_000;
  return v;
};
const scaled = (kpi: ApiKpi, v: number | null): number | null => (v === null ? null : scale(kpi, v));

export const kpiOf = (v: KpiValue): Kpi => {
  const meta = KPI[v.kpi];
  return {
    ...meta,
    better: v.betterWhen,
    // Only the hub knows the noise: inside it the screen says «как раньше», whatever the numbers.
    noise: v.verdict === 'noise' || v.verdict === 'too_few' ? Number.POSITIVE_INFINITY : 0,
    value: scaled(v.kpi, v.value),
    prev: scaled(v.kpi, v.cmpValue),
    series: [],
    prevSeries: [],
  };
};

/** The columns of «Команды рядом». */
const PEER_COLUMNS: ApiKpi[] = [
  'money_per_mr',
  'completion',
  'time_to_mr',
  'autonomy',
  'tool_errors',
  'waiting_human',
  'reread_context',
  'cache_hit',
];
/** How far from the scope's value a team has to be for the cell to be fully coloured. */
const FULL_SCORE = 0.2;

/** Each team next to the whole scope: -1 worse … 1 better, 0 when there is no number to compare. */
export const peerScore = (v: KpiValue | undefined, whole: KpiValue | undefined): number => {
  if (!v || v.value === null || !whole || whole.value === null || whole.value === 0) return 0;
  const gain = (v.value - whole.value) / Math.abs(whole.value);
  const signed = v.betterWhen === 'up' ? gain : -gain;
  return Math.max(-1, Math.min(1, signed / FULL_SCORE));
};

export const peersOf = (r: KpisResponse): Peers | null => {
  const series = r.series ?? [];
  if (series.length < 2) return null;
  return {
    title: 'Команды рядом',
    sub: 'Каждая клетка — сравнение команды со всей областью за период',
    columns: PEER_COLUMNS.map((k) => KPI[k].title),
    rows: series.map((s) => ({
      id: s.key,
      name: s.label,
      cells: PEER_COLUMNS.map((k) => {
        const v = s.kpis.find((x) => x.kpi === k);
        const whole = r.kpis.find((x) => x.kpi === k);
        return {
          text: formatValue(KPI[k].format, v ? scaled(k, v.value) : null),
          score: peerScore(v, whole),
        };
      }),
    })),
  };
};

const CHANGE_KIND: Record<ApiChange['kind'], ChangeKind> = {
  manual: 'tool',
  claude_version: 'tool',
  codex_version: 'tool',
  policy: 'tool',
  default_model: 'model',
  skills: 'plugins',
  plugins: 'plugins',
};

/** A card keeps to what moved: the changes the hub tells from noise first, at most this many. */
const EFFECTS_SHOWN = 4;

export const effectsOf = (r: EffectResponse): Effect[] => {
  const known = r.effects.flatMap((e) =>
    e.before === null || e.after === null || e.verdict === 'too_few'
      ? []
      : [
          {
            kpi: KPI[e.kpi].id,
            title: KPI[e.kpi].title,
            format: KPI[e.kpi].format,
            before: scale(e.kpi, e.before),
            after: scale(e.kpi, e.after),
            verdict: e.verdict === 'better' || e.verdict === 'worse' ? e.verdict : ('noise' as const),
          },
        ],
  );
  const moved = known.filter((e) => e.verdict !== 'noise');
  return (moved.length > 0 ? moved : known).slice(0, EFFECTS_SHOWN);
};

export const changeOf = (c: ApiChange, effect: EffectResponse | null): Change => ({
  id: c.id,
  kind: CHANGE_KIND[c.kind],
  title: c.title,
  at: c.at.slice(0, 10),
  meta: c.author?.name ?? 'хаб заметил сам',
  effects: effect ? effectsOf(effect) : [],
});

const SIGNAL_SECTION: Record<NonNullable<ApiSignal['action']>['kind'], Signal['action']['section']> = {
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
  tone: s.severity === 'critical' ? 'high' : 'mid',
  title: s.title,
  text: s.detail,
  action: s.action
    ? { label: s.action.label, section: SIGNAL_SECTION[s.action.kind] }
    : { label: 'Открыть', section: 'overview' },
});

const WASTE_TONES = ['violet', 'rose', 'amber', 'teal'] as const;

const apiIdOf = (id: KpiId): ApiKpi => {
  const found = (Object.keys(KPI) as ApiKpi[]).find((k) => KPI[k].id === id);
  if (!found) throw new OrgApiError('not_found', `unknown KPI ${id}`);
  return found;
};

const mondayOf = (now: Date): string => {
  const day = (now.getUTCDay() + 6) % 7;
  return new Date(now.getTime() - day * 86_400_000).toISOString().slice(0, 10);
};
const WEEKS = 12;

export const weeksUpTo = (now: Date): Week[] => {
  const monday = mondayOf(now);
  return Array.from({ length: WEEKS }, (_, i) => {
    const from = addDays(monday, (i - (WEEKS - 1)) * 7);
    return { from, to: addDays(from, 6) };
  });
};

/** The screen's weeks end on a Sunday; the contract's `to` is exclusive. */
const range = (w: Week) => ({ from: w.from, to: addDays(w.to, 1) });

export interface AnalyticsCtx {
  org: string;
  scope: ApiScope;
  role: Role;
  label: string;
}

const scopeParam = (c: AnalyticsCtx): string => `${c.scope.kind}:${c.scope.id}`;

export const liveAnalytics = (ctx: () => AnalyticsCtx): AnalyticsApi => ({
  analyticsFrame: async () => {
    const weeks = weeksUpTo(new Date());
    // The last finished week: the current one has only begun.
    const defaultPeriod = weeks[weeks.length - 2] ?? weeks[0];
    if (!defaultPeriod) throw new OrgApiError('internal', 'no weeks');
    const c = ctx();
    let lastChange = null;
    if (c.role !== 'member') {
      const { changes } = await contractGet<ChangesResponse>(orgPath(c.org, '/changes'), {
        scope: scopeParam(c),
      });
      const last = [...changes].sort((x, y) => x.at.localeCompare(y.at)).pop();
      lastChange = last ? { id: last.id, title: last.title, at: last.at.slice(0, 10) } : null;
    }
    return { weeks, defaultPeriod, lastChange };
  },
  analytics: async ({ period, compare }): Promise<AnalyticsData> => {
    const c = ctx();
    const cmp = { cmpFrom: compare.from, cmpTo: addDays(compare.to, 1) };
    if (c.role === 'member') {
      const [kpis, usage, sessions] = await Promise.all([
        http.get<KpisResponse>('/api/me/kpis', { ...range(period), ...cmp }),
        http.get<MeUsageResponse>('/api/me/usage', range(period)),
        allSessions(range(period)),
      ]);
      const live = sessions.filter((s) => s.origin === 'live');
      return {
        scopeLabel: 'Только твоя работа',
        summary: {
          spendUsd: spentOf(usage.total).usd,
          sessions: live.length,
          mrs: live.reduce((n, s) => n + s.mrs, 0),
        },
        kpis: kpis.kpis.map(kpiOf),
        signals: [],
        changes: [],
        waste: null,
        peers: null,
      };
    }
    const scope = scopeParam(c);
    const [kpis, overview, waste, changes, signals] = await Promise.all([
      contractGet<KpisResponse>(orgPath(c.org, '/kpis'), { scope, ...range(period), ...cmp, series: 'team' }),
      contractGet<OverviewResponse>(orgPath(c.org, '/overview'), { scope, ...range(period) }),
      contractGet<WasteResponse>(orgPath(c.org, '/waste'), { scope, ...range(period) }),
      contractGet<ChangesResponse>(orgPath(c.org, '/changes'), { scope, ...range(period) }),
      contractGet<SignalsResponse>(orgPath(c.org, '/signals'), { scope }),
    ]);
    const effects = await Promise.all(
      changes.changes.map((ch) =>
        contractGet<EffectResponse>(orgPath(c.org, `/changes/${ch.id}/effect`), { scope }),
      ),
    );
    const items = waste.items.map((w, i) => ({
      id: w.kind,
      title: w.title,
      text: w.hint,
      usd: w.amount.usd,
      tone: WASTE_TONES[i % WASTE_TONES.length] ?? 'violet',
    }));
    return {
      scopeLabel: c.label,
      summary: {
        spendUsd: spentOf(overview.spend).usd,
        sessions: overview.tiles.sessions.value ?? 0,
        mrs: overview.tiles.mrs.value ?? 0,
      },
      kpis: kpis.kpis.map(kpiOf),
      signals: signals.signals.map(signalOf),
      changes: changes.changes.map((ch, i) => changeOf(ch, effects[i] ?? null)),
      waste: items.length
        ? { wasteUsd: items.reduce((n, w) => n + w.usd, 0), totalUsd: overview.spend.list.usd, items }
        : null,
      peers: peersOf(kpis),
    };
  },
  analyticsChart: async ({ period, compare }, kpi): Promise<KpiChart> => {
    const c = ctx();
    if (c.role === 'member') throw new OrgApiError('not_ready', 'no daily KPI for a person yet');
    const api = apiIdOf(kpi);
    const path = orgPath(c.org, `/kpis/${api}/series`);
    const scope = scopeParam(c);
    const [now, was] = await Promise.all([
      contractGet<KpiSeriesResponse>(path, { scope, ...range(period), by: 'day', split: 'team' }),
      contractGet<KpiSeriesResponse>(path, { scope, ...range(compare), by: 'day' }),
    ]);
    const days = now.points.map((p) => p.day);
    // A bucket of too few sessions is a gap in the line, never a zero.
    const value = (v: number | null) => scaled(api, v);
    const lines = (now.splits ?? [{ key: 'all', label: c.label, points: now.points }]).map((s) => ({
      id: s.key,
      name: s.label,
      values: s.points.map((p) => value(p.value)),
    }));
    return {
      kpi,
      days,
      lines,
      compareLine: was.points.map((p) => value(p.value)),
      periodFrom: 0,
      periodTo: Math.max(0, days.length - 1),
      markers: now.markers.flatMap((m) => {
        const index = days.indexOf(m.at.slice(0, 10));
        return index < 0 ? [] : [{ id: m.changeId, kind: 'tool' as const, title: m.title, index }];
      }),
    };
  },
});
