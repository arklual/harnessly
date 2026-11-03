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
  WasteItem as ApiWasteItem,
  WasteResponse,
} from '../../../../hub-api/analytics';
import type { Scope as ApiScope } from '../../../../hub-api/orgs';
import { addDays, daysBetween, isoDay } from '../../dates';
import type { Role } from '../../types';
import { OrgApiError } from '../errors';
import type {
  AnalyticsApi,
  AnalyticsData,
  Change,
  ChangeKind,
  DetailRow,
  Effect,
  DeltaKind,
  Kpi,
  KpiChart,
  KpiFormat,
  KpiGroup,
  KpiId,
  Marker,
  Peers,
  Signal,
  Week,
} from '../analytics';
import { formatValue } from '../../screens/analytics/format';
import { spentOf } from '../../money';
import { contractGet, http, orgPath } from './http';
import { signalGo } from './signal-go';
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
    hint: 'Сколько стоил один смёрженный MR по прайсу API: токены чатов, где он сделан, по цене модели.',
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
    series: (v.spark ?? []).map((x) => scaled(v.kpi, x)),
    prevSeries: (v.cmpSpark ?? []).map((x) => scaled(v.kpi, x)),
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
          // What the cell is measured against, for its bubble: the scope's own number.
          mean: formatValue(KPI[k].format, whole ? scaled(k, whole.value) : null),
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
  env: 'plugins', // the environment's skills, MCP servers, repositories and documents show with the plugins
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

/** Who made the change; the hub's answer does not say which layer of the environment it sits in. */
export const metaOf = (c: Pick<ApiChange, 'author'>): string => c.author?.name ?? 'хаб заметил сам';

export const changeOf = (c: ApiChange, effect: EffectResponse | null): Change => ({
  id: c.id,
  kind: CHANGE_KIND[c.kind],
  title: c.title,
  at: c.at.slice(0, 10),
  meta: metaOf(c),
  effects: effect ? effectsOf(effect) : [],
});

/** The numbers the hub keeps behind a signal, for its bubble; nothing is made up when it has none. */
export const signalDetail = (s: ApiSignal): DetailRow[] => [
  { label: 'О чём', value: s.subject.label },
  { label: 'Видно с', value: s.firstAt.slice(0, 10).split('-').reverse().join('.') },
  ...(s.escalated ? [{ label: 'Уровень', value: 'поднят выше: никто не среагировал 48 ч' }] : []),
];

const signalOf = (s: ApiSignal): Signal => ({
  id: s.id,
  tone: s.severity === 'critical' ? 'high' : 'mid',
  title: s.title,
  text: s.detail,
  detail: signalDetail(s),
  action: signalGo(s.action),
});

/** Where a waste number comes from: the sessions behind it and its part of the list spend. */
export const wasteDetail = (w: ApiWasteItem): DetailRow[] => [
  { label: 'Сессий', value: String(w.sessions) },
  { label: 'Доля расхода', value: `${Math.round(w.share * 100)}%` },
];

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

/** The days the big chart draws: a month ending a week after the period, never past today. */
export const chartWindow = (period: Week, today: string): { from: string; to: string } => {
  const to = [addDays(period.to, 8), addDays(today, 1)].sort()[0] ?? addDays(today, 1);
  return { from: addDays(to, -30), to };
};

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
    // The strip's bars need the money of every week and the hub has no such answer: they stay at the minimum.
    const last = weeks.length - 1;
    return {
      weeks: weeks.map((w, i) => (i === last ? { ...w, current: true } : w)),
      today: isoDay(new Date()),
      defaultPeriod,
      lastChange,
    };
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
      detail: wasteDetail(w),
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
    const win = chartWindow(period, isoDay(new Date()));
    const shift = daysBetween(period.from, compare.from);
    const before = { from: addDays(win.from, shift), to: addDays(win.to, shift) };
    const [now, was, changes] = await Promise.all([
      contractGet<KpiSeriesResponse>(path, { scope, ...win, by: 'day', split: 'team' }),
      contractGet<KpiSeriesResponse>(path, { scope, ...before, by: 'day' }),
      contractGet<ChangesResponse>(orgPath(c.org, '/changes'), { scope, ...win }),
    ]);
    const days = now.points.map((p) => p.day);
    // A bucket of too few sessions is a gap in the line, never a zero.
    const value = (v: number | null) => scaled(api, v);
    const lines = (now.splits ?? [{ key: 'all', label: c.label, points: now.points }]).map((s) => ({
      id: s.key,
      name: s.label,
      values: s.points.map((p) => value(p.value)),
    }));
    const periodFrom = Math.max(0, days.indexOf(period.from));
    const at = days.indexOf(period.to);
    const periodTo = at < 0 ? Math.max(0, days.length - 1) : at;
    const previous = was.points.map((p) => value(p.value));
    const byId = new Map(changes.changes.map((ch) => [ch.id, ch]));
    return {
      kpi,
      days,
      lines,
      total: now.points.map((p) => value(p.value)),
      previous,
      compareLine: previous.slice(periodFrom, periodTo + 1),
      periodFrom,
      periodTo,
      markers: now.markers.flatMap((m): Marker[] => {
        const index = days.indexOf(m.at.slice(0, 10));
        if (index < 0) return [];
        const ch = byId.get(m.changeId);
        return [
          {
            id: m.changeId,
            kind: ch ? CHANGE_KIND[ch.kind] : 'tool',
            title: m.title,
            index,
            detail: ch ? metaOf(ch) : undefined,
          },
        ];
      }),
    };
  },
});
