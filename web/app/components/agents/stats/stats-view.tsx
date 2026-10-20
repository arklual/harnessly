import { issueUrlOf } from '@/lib/site';
import { locale, plural, t } from '@/lib/i18n';
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { CircleHelp, RefreshCw, TriangleAlert } from 'lucide-react';
import { appFetch } from '@/lib/app-path';
import useIsMobile from '@/hooks/use-is-mobile';
import { formatModelDisplayName } from '@/lib/claude-tokens';
import type { ITeamUsage } from '@/lib/hl-usage/types';
import EChart from '@/components/agents/stats/echart';
import {
  activityByDay,
  cacheHitRate,
  change,
  factsIn,
  historyStart,
  leaderboard,
  mergeTeam,
  personColorIndex,
  personName,
  projectBars,
  punchcard,
  ratio,
  seriesByKey,
  sessionsIn,
  shiftDay,
  taskRows,
  todayKey,
  totals,
  windowFor,
  type IFilters,
  type IPersonSession,
  type ITeamData,
  type ITotals,
  type TPeriod,
  type TProviderFilter,
} from '@/components/agents/stats/aggregate';
import {
  baseAnimation,
  baseTooltip,
  categoryAxis,
  clickableLegend,
  colorAt,
  dashedAxisPointer,
  itemHoverState,
  useChartTheme,
  valueAxis,
  verticalGradient,
  withAlpha,
  type IChartTheme,
} from '@/components/agents/stats/chart-theme';

// --- formatting ---

const RU = locale() === 'ru';
const LOCALE_TAG = RU ? 'ru-RU' : 'en-US';
const nf = new Intl.NumberFormat(LOCALE_TAG);
const n0 = (v: number) => nf.format(Math.round(v));
/** A fixed-point number with the page language's decimal mark. */
const fixed = (v: number, digits: number) => (RU ? v.toFixed(digits).replace('.', ',') : v.toFixed(digits));
const money = (v: number) => (v >= 100 ? `$${n0(v)}` : `$${fixed(v, v >= 10 ? 1 : 2)}`);
const tokens = (v: number) => {
  if (v >= 1e9) return t('stats.statsView.billions', { n: fixed(v / 1e9, 1) });
  if (v >= 1e6) return t('stats.statsView.millions', { n: fixed(v / 1e6, 1) });
  if (v >= 1e3) return t('stats.statsView.thousands', { n: fixed(v / 1e3, 1) });
  return n0(v);
};
const pct = (v: number | null, digits = 0) => (v === null ? '—' : `${fixed(v * 100, digits)}%`);
const hours = (v: number) => t('stats.statsView.hours', { n: v >= 10 ? n0(v) : fixed(v, 1) });
const MONTHS = t('stats.statsView.months').split(',');
const shortDay = (day: string) => `${Number(day.slice(8, 10))} ${MONTHS[Number(day.slice(5, 7)) - 1]}`;
const WEEKDAYS = t('stats.statsView.weekdays').split(',');
const modelName = (model: string) => (model.startsWith('claude') ? formatModelDisplayName(model) : model);

const PERIODS: { value: TPeriod; label: string }[] = [
  { value: 7, label: t('stats.statsView.period7') },
  { value: 30, label: t('stats.statsView.period30') },
  { value: 90, label: t('stats.statsView.period90') },
  { value: 365, label: t('stats.statsView.periodYear') },
];
const PROVIDERS: { value: TProviderFilter; label: string }[] = [
  { value: 'all', label: t('stats.statsView.allAgents') },
  { value: 'claude', label: 'Claude' },
  { value: 'codex', label: 'Codex' },
];

// --- data ---

const useTeamUsage = () => {
  const [team, setTeam] = useState<ITeamUsage | null>(null);
  const [error, setError] = useState('');
  const [requested, setRequested] = useState(0);
  const [settled, setSettled] = useState(-1);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await appFetch('/api/platform/stats');
        if (!res.ok)
          throw new Error(
            res.status === 403 ? t('stats.statsView.errorNoAccess') : t('stats.statsView.errorLoad'),
          );
        const data = (await res.json()) as ITeamUsage;
        if (cancelled) return;
        setTeam(data);
        setError('');
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : t('stats.statsView.errorLoad'));
      } finally {
        if (!cancelled) setSettled(requested);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [requested]);

  const reload = useCallback(() => setRequested((count) => count + 1), []);

  useEffect(() => {
    if (!team) return undefined;
    const collecting = team.runtimes.some((r) => r.report?.collecting);
    const timer = window.setTimeout(reload, collecting ? 10_000 : 300_000);
    return () => window.clearTimeout(timer);
  }, [team, reload]);

  return { team, error, loading: settled !== requested, reload };
};

// --- building blocks ---

const Hint = ({ text }: { text: string }) => (
  <span className="ts-hint" tabIndex={0} aria-label={text} data-tip={text}>
    <CircleHelp aria-hidden="true" />
  </span>
);

interface IPanelProps {
  title: string;
  sub?: ReactNode;
  span: 4 | 5 | 6 | 7 | 8 | 12;
  hint?: string;
  children: ReactNode;
}

const Panel = ({ title, sub, span, hint, children }: IPanelProps) => (
  <section className={`ts-panel ts-span-${span}`}>
    <header className="ts-panel-head">
      <div>
        <h2 className="ts-panel-title">
          {title}
          {hint && <Hint text={hint} />}
        </h2>
        {sub && <p className="ts-panel-sub">{sub}</p>}
      </div>
    </header>
    <div className="ts-panel-body">{children}</div>
  </section>
);

const Empty = ({ height, text = t('stats.statsView.emptyPeriod') }: { height: number; text?: string }) => (
  <div className="ts-empty" style={{ height }}>
    {text}
  </div>
);

interface ITileProps {
  label: string;
  value: string;
  delta: number | null;
  sub?: string;
  hint: string;
  /** For costs and errors a rise is bad news, so the colour flips. */
  inverse?: boolean;
}

const Tile = ({ label, value, delta, sub, hint, inverse }: ITileProps) => {
  const good = delta === null || Math.abs(delta) < 0.005 ? null : delta > 0 !== !!inverse;
  return (
    <div className="ts-tile">
      <span className="ts-tile-label">
        {label}
        <Hint text={hint} />
      </span>
      <strong className="ts-tile-value">{value}</strong>
      <span className="ts-tile-foot">
        {delta !== null && (
          <span className={`ts-trend${good === null ? '' : good ? ' ts-trend-up' : ' ts-trend-down'}`}>
            {delta > 0 ? '↑' : delta < 0 ? '↓' : '→'} {pct(Math.abs(delta))}
          </span>
        )}
        {sub && <span className="ts-tile-sub">{sub}</span>}
      </span>
    </div>
  );
};

interface ISegmentedProps<T extends string | number> {
  label: string;
  value: T;
  options: { value: T; label: string }[];
  onChange: (value: T) => void;
}

const Segmented = <T extends string | number>({ label, value, options, onChange }: ISegmentedProps<T>) => (
  <div className="ts-segmented" role="radiogroup" aria-label={label}>
    {options.map((o) => (
      <button
        key={o.value}
        type="button"
        role="radio"
        aria-checked={o.value === value}
        className={o.value === value ? 'ts-seg-on' : ''}
        onClick={() => onChange(o.value)}
      >
        {o.label}
      </button>
    ))}
  </div>
);

// --- chart options ---

const tooltipRows = (rows: { color?: string; label: string; value: string }[], head: string) =>
  `<div style="font-weight:600;margin-bottom:6px">${head}</div>` +
  rows
    .map(
      (r) =>
        `<div style="display:flex;align-items:center;gap:8px;justify-content:space-between;min-width:170px"><span style="display:inline-flex;align-items:center;gap:6px">${r.color ? `<span style="width:8px;height:8px;border-radius:3px;background:${r.color}"></span>` : ''}${r.label}</span><b style="font-variant-numeric:tabular-nums">${r.value}</b></div>`,
    )
    .join('');

const stackedBars = (
  th: IChartTheme,
  days: string[],
  series: { name: string; color: string; values: number[] }[],
  format: (v: number) => string,
) => ({
  ...baseAnimation(),
  grid: { left: 8, right: 8, top: 36, bottom: 4, containLabel: true },
  legend: clickableLegend(th, { data: series.map((s) => s.name) }),
  tooltip: baseTooltip(th, {
    trigger: 'axis',
    axisPointer: { type: 'shadow', shadowStyle: { color: withAlpha(th.axis, 0.06) } },
    formatter: (params: { dataIndex: number }[]) => {
      const at = params[0]?.dataIndex ?? 0;
      const rows = series
        .filter((s) => s.values[at] > 0)
        .map((s) => ({ color: s.color, label: s.name, value: format(s.values[at]) }));
      const sum = series.reduce((acc, s) => acc + s.values[at], 0);
      return tooltipRows(
        series.length > 1 ? [...rows, { label: t('stats.statsView.total'), value: format(sum) }] : rows,
        shortDay(days[at]),
      );
    },
  }),
  xAxis: categoryAxis(th, days.map(shortDay)),
  yAxis: valueAxis(th, {
    axisLabel: {
      color: th.axis,
      fontFamily: th.fontFamily,
      fontSize: 11,
      formatter: (v: number) => format(v),
    },
  }),
  series: series.map((s, i) => ({
    name: s.name,
    type: 'bar',
    stack: 'total',
    barMaxWidth: 22,
    data: s.values,
    itemStyle: {
      color: s.color,
      // Only the top segment of the stack gets the rounded 4px data-end.
      borderRadius: i === series.length - 1 ? [4, 4, 0, 0] : 0,
      borderColor: th.surface,
      borderWidth: series.length > 1 ? 1 : 0,
    },
    ...itemHoverState(undefined, 0.35),
  })),
});

const calendarOption = (th: IChartTheme, values: Map<string, number>, end: string, days: number) => {
  const start = shiftDay(end, -(days - 1));
  const data: [string, number][] = [];
  for (let d = start; d <= end; d = shiftDay(d, 1)) data.push([d, values.get(d) ?? 0]);
  const max = Math.max(1, ...data.map(([, v]) => v));
  // Quartile steps so one marathon day doesn't wash out the rest.
  const positive = data
    .map(([, v]) => v)
    .filter((v) => v > 0)
    .sort((a, b) => a - b);
  const q = (p: number) => positive[Math.min(positive.length - 1, Math.floor(p * positive.length))] ?? max;
  const steps = [1, q(0.25), q(0.5), q(0.75)].map((v, i, all) => Math.max(v, (all[i - 1] ?? 0) + 1));
  return {
    ...baseAnimation(),
    tooltip: baseTooltip(th, {
      formatter: (p: { value: [string, number] }) =>
        tooltipRows(
          [{ label: t('stats.statsView.promptsLabel'), value: n0(p.value[1]) }],
          `${shortDay(p.value[0])} ${p.value[0].slice(0, 4)}`,
        ),
    }),
    visualMap: {
      type: 'piecewise',
      show: false,
      pieces: [
        { max: 0, color: th.ramp[0] },
        { min: steps[0], max: steps[1] - 1, color: th.ramp[1] },
        { min: steps[1], max: steps[2] - 1, color: th.ramp[2] },
        { min: steps[2], max: steps[3] - 1, color: th.ramp[3] },
        { min: steps[3], color: th.ramp[4] },
      ],
    },
    calendar: {
      range: [start, end],
      top: 22,
      left: 34,
      right: 6,
      bottom: 4,
      cellSize: ['auto', 14],
      orient: 'horizontal',
      splitLine: { show: false },
      itemStyle: { color: th.ramp[0], borderColor: th.surface, borderWidth: 3, borderRadius: 3 },
      yearLabel: { show: false },
      dayLabel: {
        firstDay: 1,
        nameMap: [WEEKDAYS[6], ...WEEKDAYS.slice(0, 6)],
        color: th.axis,
        fontSize: 10,
        fontFamily: th.fontFamily,
      },
      monthLabel: { nameMap: MONTHS, color: th.axis, fontSize: 11, fontFamily: th.fontFamily },
    },
    series: [
      {
        type: 'heatmap',
        coordinateSystem: 'calendar',
        data,
        itemStyle: { borderRadius: 3, borderColor: th.surface, borderWidth: 3 },
        emphasis: { itemStyle: { borderColor: th.text, borderWidth: 1 } },
      },
    ],
  };
};

const punchcardOption = (th: IChartTheme, grid: number[][]) => {
  const data: [number, number, number][] = [];
  let max = 1;
  grid.forEach((row, weekday) =>
    row.forEach((v, hour) => {
      data.push([hour, weekday, v]);
      max = Math.max(max, v);
    }),
  );
  return {
    ...baseAnimation(),
    grid: { left: 30, right: 6, top: 6, bottom: 22 },
    tooltip: baseTooltip(th, {
      formatter: (p: { value: [number, number, number] }) =>
        tooltipRows(
          [{ label: t('stats.statsView.promptsLabel'), value: n0(p.value[2]) }],
          `${WEEKDAYS[p.value[1]]}, ${p.value[0]}:00–${p.value[0] + 1}:00`,
        ),
    }),
    xAxis: {
      type: 'category',
      data: Array.from({ length: 24 }, (_, h) => String(h)),
      splitArea: { show: false },
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: th.axis, fontSize: 10, interval: 2, fontFamily: th.fontFamily },
    },
    yAxis: {
      type: 'category',
      data: WEEKDAYS,
      inverse: true,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: th.axis, fontSize: 11, fontFamily: th.fontFamily },
    },
    visualMap: { show: false, min: 0, max, inRange: { color: th.ramp } },
    series: [
      {
        type: 'heatmap',
        data,
        itemStyle: { borderColor: th.surface, borderWidth: 2, borderRadius: 3 },
        emphasis: { itemStyle: { borderColor: th.text, borderWidth: 1 } },
      },
    ],
  };
};

const horizontalBars = (
  th: IChartTheme,
  bars: { name: string; value: number; extra: Record<string, number> }[],
) => ({
  ...baseAnimation(),
  grid: { left: 8, right: 56, top: 4, bottom: 4, containLabel: true },
  tooltip: baseTooltip(th, {
    trigger: 'item',
    formatter: (p: { dataIndex: number }) => {
      const b = bars[p.dataIndex];
      return tooltipRows(
        [
          { label: t('stats.statsView.cost'), value: money(b.value) },
          { label: t('stats.statsView.modelReplies'), value: n0(b.extra.calls) },
          { label: t('stats.statsView.outputTokens'), value: tokens(b.extra.output) },
        ],
        b.name,
      );
    },
  }),
  xAxis: { type: 'value', show: false },
  yAxis: {
    type: 'category',
    data: bars.map((b) => b.name),
    inverse: true,
    axisLine: { show: false },
    axisTick: { show: false },
    axisLabel: { color: th.text, fontSize: 12, fontFamily: th.fontFamily, width: 130, overflow: 'truncate' },
  },
  series: [
    {
      type: 'bar',
      data: bars.map((b, i) => ({
        value: b.value,
        itemStyle: { color: i === 0 ? colorAt(th, 0) : withAlpha(colorAt(th, 0), 0.55) },
      })),
      barMaxWidth: 16,
      itemStyle: { borderRadius: [0, 4, 4, 0] },
      label: {
        show: true,
        position: 'right',
        color: th.axis,
        fontSize: 11,
        fontFamily: th.fontFamily,
        formatter: (p: { value: number }) => money(p.value),
      },
      ...itemHoverState(undefined, 0.5),
    },
  ],
});

const rateLine = (
  th: IChartTheme,
  days: string[],
  name: string,
  values: (number | null)[],
  color: string,
) => ({
  ...baseAnimation(),
  grid: { left: 8, right: 12, top: 12, bottom: 4, containLabel: true },
  tooltip: baseTooltip(th, {
    trigger: 'axis',
    axisPointer: dashedAxisPointer(th),
    formatter: (params: { dataIndex: number }[]) => {
      const at = params[0]?.dataIndex ?? 0;
      return tooltipRows([{ color, label: name, value: pct(values[at], 1) }], shortDay(days[at]));
    },
  }),
  xAxis: categoryAxis(th, days.map(shortDay), { boundaryGap: false }),
  yAxis: valueAxis(th, {
    axisLabel: { color: th.axis, fontSize: 11, fontFamily: th.fontFamily, formatter: (v: number) => pct(v) },
  }),
  series: [
    {
      name,
      type: 'line',
      data: values,
      connectNulls: true,
      smooth: 0.25,
      symbol: 'circle',
      symbolSize: 6,
      showSymbol: false,
      lineStyle: { width: 2, color },
      itemStyle: { color, borderColor: th.surface, borderWidth: 2 },
      areaStyle: { color: verticalGradient(color, 0.22, 0.01) },
    },
  ],
});

// --- view ---

const StatsView = () => {
  const { team, error, loading, reload } = useTeamUsage();
  const theme = useChartTheme();
  const [period, setPeriod] = useState<TPeriod>(30);
  const [people, setPeople] = useState<string[]>([]);
  const [provider, setProvider] = useState<TProviderFilter>('all');
  const [sessionSort, setSessionSort] = useState<'cost' | 'prompts' | 'end'>('cost');

  const data: ITeamData | null = useMemo(() => (team ? mergeTeam(team) : null), [team]);
  const filters: IFilters = useMemo(() => ({ period, people, provider }), [period, people, provider]);
  const end = todayKey();
  const w = useMemo(() => windowFor(period, end), [period, end]);
  const prev = useMemo(() => windowFor(period, end, 1), [period, end]);

  const cur = useMemo(() => (data ? totals(data, filters, w) : null), [data, filters, w]);
  const comparable = useMemo(() => {
    const start = data ? historyStart(data, filters) : null;
    return !!start && start <= prev.from;
  }, [data, filters, prev]);
  const before = useMemo(() => (data ? totals(data, filters, prev) : null), [data, filters, prev]);

  const togglePerson = (person: string) =>
    setPeople((current) =>
      current.includes(person) ? current.filter((p) => p !== person) : [...current, person],
    );

  if (error && !team) {
    return (
      <div className="ts-root">
        <div className="ts-state">
          <TriangleAlert /> {error}
          <button type="button" className="ts-btn" onClick={() => void reload()}>
            {t('stats.statsView.retry')}
          </button>
        </div>
      </div>
    );
  }
  if (!data || !cur || !before || !theme) {
    return (
      <div className="ts-root" aria-busy="true">
        <div className="ts-head">
          <div>
            <h1 className="ts-title">{t('stats.statsView.title')}</h1>
            <p className="ts-sub">{t('stats.statsView.collecting')}</p>
          </div>
        </div>
        {!comparable && <p className="ts-note">{t('stats.statsView.noComparison')}</p>}
        <div className="ts-tiles">
          {Array.from({ length: 6 }, (_, i) => (
            <div key={i} className="ts-tile ts-skeleton" />
          ))}
        </div>
        <div className="ts-grid">
          <div className="ts-panel ts-span-12 ts-skeleton" style={{ height: 220 }} />
          <div className="ts-panel ts-span-8 ts-skeleton" style={{ height: 320 }} />
          <div className="ts-panel ts-span-4 ts-skeleton" style={{ height: 320 }} />
        </div>
      </div>
    );
  }

  return (
    <StatsBody
      data={data}
      theme={theme}
      filters={filters}
      w={w}
      cur={cur}
      before={before}
      period={period}
      setPeriod={setPeriod}
      provider={provider}
      setProvider={setProvider}
      togglePerson={togglePerson}
      clearPeople={() => setPeople([])}
      loading={loading}
      reload={() => void reload()}
      comparable={comparable}
      sessionSort={sessionSort}
      setSessionSort={setSessionSort}
      generatedAt={team?.runtimes.find((r) => r.report)?.report?.generatedAt ?? null}
    />
  );
};

interface IBodyProps {
  data: ITeamData;
  theme: IChartTheme;
  filters: IFilters;
  w: ReturnType<typeof windowFor>;
  cur: ITotals;
  before: ITotals;
  period: TPeriod;
  setPeriod: (p: TPeriod) => void;
  provider: TProviderFilter;
  setProvider: (p: TProviderFilter) => void;
  togglePerson: (p: string) => void;
  clearPeople: () => void;
  loading: boolean;
  reload: () => void;
  comparable: boolean;
  sessionSort: 'cost' | 'prompts' | 'end';
  setSessionSort: (s: 'cost' | 'prompts' | 'end') => void;
  generatedAt: string | null;
}

const StatsBody = ({
  data,
  theme: th,
  filters,
  w,
  cur,
  before,
  period,
  setPeriod,
  provider,
  setProvider,
  togglePerson,
  clearPeople,
  loading,
  reload,
  comparable,
  sessionSort,
  setSessionSort,
  generatedAt,
}: IBodyProps) => {
  const personColor = useCallback(
    (person: string) => colorAt(th, personColorIndex(person, data.people.indexOf(person))),
    [th, data.people],
  );
  const picked = filters.people.length ? data.people.filter((p) => filters.people.includes(p)) : data.people;

  const promptsByPerson = useMemo(
    () =>
      picked.map((person) => {
        const byDay = activityByDay(data, filters, w, person);
        return {
          name: personName(person),
          color: personColor(person),
          values: w.days.map((d) => byDay.get(d)?.prompts ?? 0),
        };
      }),
    [data, filters, w, picked, personColor],
  );

  const board = useMemo(() => leaderboard(data, filters, w), [data, filters, w]);
  const boardMax = Math.max(1, ...board.map((r) => r.prompts));

  const facts = useMemo(() => factsIn(data, filters, w), [data, filters, w]);
  const costByModel = useMemo(
    () =>
      seriesByKey(
        facts,
        w,
        (f) => modelName(f.model),
        (f) => f.cost,
        5,
      ).map((s) => ({ name: s.name, color: colorAt(th, s.colorIndex), values: s.values })),
    [facts, w, th],
  );

  const hoursByPerson = useMemo(
    () =>
      picked.map((person) => {
        const byDay = activityByDay(data, filters, w, person);
        return {
          name: personName(person),
          color: personColor(person),
          values: w.days.map((d) => (byDay.get(d)?.turnMs ?? 0) / 3_600_000),
        };
      }),
    [data, filters, w, picked, personColor],
  );

  const calendarValues = useMemo(() => {
    const year = windowFor(365);
    const byDay = activityByDay(data, filters, year);
    return new Map([...byDay.entries()].map(([d, v]) => [d, v.prompts]));
  }, [data, filters]);
  const yearActiveDays = [...calendarValues.values()].filter((v) => v > 0).length;
  const yearPrompts = [...calendarValues.values()].reduce((a, b) => a + b, 0);
  const streak = useMemo(() => {
    let best = 0,
      run = 0,
      current = 0;
    const days = windowFor(365).days;
    for (const d of days) {
      if ((calendarValues.get(d) ?? 0) > 0) {
        run++;
        best = Math.max(best, run);
      } else run = 0;
    }
    for (let i = days.length - 1; i >= 0; i--) {
      if ((calendarValues.get(days[i]) ?? 0) > 0) current++;
      else if (i < days.length - 1) break;
    }
    return { best, current };
  }, [calendarValues]);

  const punch = useMemo(() => punchcard(data, filters, w), [data, filters, w]);
  const projects = useMemo(() => projectBars(data, filters, w, 10), [data, filters, w]);

  const quality = useMemo(() => {
    const byDay = activityByDay(data, filters, w);
    return {
      errors: w.days.map((d) => {
        const v = byDay.get(d);
        return v ? ratio(v.toolErrors, v.tools) : null;
      }),
    };
  }, [data, filters, w]);
  const cacheByDay = useMemo(
    () =>
      w.days.map((d) => {
        const dayFacts = facts.filter((f) => f.day === d);
        return cacheHitRate(
          dayFacts.reduce(
            (a, f) => ({
              input: a.input + f.input,
              cacheRead: a.cacheRead + f.cacheRead,
              cacheWrite: a.cacheWrite + f.cacheWrite,
            }),
            { input: 0, cacheRead: 0, cacheWrite: 0 },
          ),
        );
      }),
    [facts, w],
  );

  const sessions = useMemo(() => sessionsIn(data, filters, w), [data, filters, w]);
  const tasks = useMemo(() => taskRows(sessions).slice(0, 12), [sessions]);
  const topSessions = useMemo(
    () =>
      [...sessions]
        .sort((a, b) =>
          sessionSort === 'cost'
            ? b.cost - a.cost
            : sessionSort === 'prompts'
              ? b.prompts - a.prompts
              : b.end - a.end,
        )
        .slice(0, 15),
    [sessions, sessionSort],
  );

  const promptsOption = useMemo(() => stackedBars(th, w.days, promptsByPerson, n0), [th, w, promptsByPerson]);
  const [costBy, setCostBy] = useState<'model' | 'person'>('model');
  const costByPerson = useMemo(
    () =>
      picked.map((person) => {
        const values = w.days.map(() => 0);
        const index = new Map(w.days.map((d, i) => [d, i]));
        for (const f of facts)
          if (f.person === person) {
            const at = index.get(f.day);
            if (at !== undefined) values[at] += f.cost;
          }
        return { name: personName(person), color: personColor(person), values };
      }),
    [facts, w, picked, personColor],
  );
  const costSeries = costBy === 'model' ? costByModel : costByPerson;
  const costOption = useMemo(() => stackedBars(th, w.days, costSeries, money), [th, w, costSeries]);
  const hoursOption = useMemo(() => stackedBars(th, w.days, hoursByPerson, hours), [th, w, hoursByPerson]);
  const isMobile = useIsMobile();
  const calendarDays = isMobile ? 120 : 365;
  const calendarOpt = useMemo(
    () => calendarOption(th, calendarValues, todayKey(), calendarDays),
    [th, calendarValues, calendarDays],
  );
  const punchOption = useMemo(() => punchcardOption(th, punch), [th, punch]);
  const projectsOption = useMemo(() => horizontalBars(th, projects), [th, projects]);
  const errorsOption = useMemo(
    () => rateLine(th, w.days, t('stats.statsView.errorsSeries'), quality.errors, colorAt(th, 5)),
    [th, w, quality],
  );
  const cacheOption = useMemo(
    () => rateLine(th, w.days, t('stats.statsView.cacheHit'), cacheByDay, colorAt(th, 2)),
    [th, w, cacheByDay],
  );

  const hit = cacheHitRate(cur);
  const hitBefore = cacheHitRate(before);
  const errRate = ratio(cur.toolErrors, cur.tools);
  const errBefore = ratio(before.toolErrors, before.tools);
  const periodLabel = PERIODS.find((p) => p.value === period)?.label.toLowerCase() ?? '';
  const noPrompts = cur.prompts === 0 && cur.cost === 0;

  return (
    <div className="ts-root">
      <div className="ts-head">
        <div>
          <h1 className="ts-title">{t('stats.statsView.title')}</h1>
          <p className="ts-sub">
            {t('stats.statsView.subtitle')}
            {generatedAt &&
              t('stats.statsView.updatedAt', {
                time: new Date(generatedAt).toLocaleTimeString(LOCALE_TAG, {
                  hour: '2-digit',
                  minute: '2-digit',
                }),
              })}
          </p>
        </div>
        <button
          type="button"
          className="ts-icon-btn"
          onClick={reload}
          disabled={loading}
          title={t('stats.statsView.refresh')}
          aria-label={t('stats.statsView.refreshStats')}
        >
          <RefreshCw className={loading ? 'ts-spin' : ''} />
        </button>
      </div>

      <div className="ts-filters">
        <Segmented
          label={t('stats.statsView.period')}
          value={period}
          options={PERIODS}
          onChange={setPeriod}
        />
        <div className="ts-people" role="group" aria-label={t('stats.statsView.people')}>
          <button
            type="button"
            className={`ts-chip${filters.people.length === 0 ? ' ts-chip-on' : ''}`}
            onClick={clearPeople}
          >
            {t('stats.statsView.wholeTeam')}
          </button>
          {data.people.map((person) => (
            <button
              key={person}
              type="button"
              className={`ts-chip${filters.people.includes(person) ? ' ts-chip-on' : ''}`}
              aria-pressed={filters.people.includes(person)}
              onClick={() => togglePerson(person)}
            >
              <span className="ts-dot" style={{ background: personColor(person) }} />
              {personName(person)}
            </button>
          ))}
        </div>
        <Segmented
          label={t('stats.statsView.agent')}
          value={provider}
          options={PROVIDERS}
          onChange={setProvider}
        />
      </div>

      {data.collecting.length > 0 && (
        <div className="ts-banner" role="status">
          {t('stats.statsView.firstPass', {
            machines: data.collecting
              .map((c) =>
                plural(c.total, 'stats.statsView.machineFiles', { machine: c.machine, done: c.done }),
              )
              .join(', '),
          })}
        </div>
      )}
      {data.unavailable.length > 0 && (
        <div className="ts-banner ts-banner-warn" role="status">
          {t('stats.statsView.unavailable', { machines: data.unavailable.join(', ') })}
        </div>
      )}

      {!comparable && <p className="ts-note">{t('stats.statsView.noComparison')}</p>}
      <div className="ts-tiles">
        <Tile
          label={t('stats.statsView.prompts')}
          value={n0(cur.prompts)}
          delta={comparable ? change(cur.prompts, before.prompts) : null}
          sub={plural(cur.activeDays, 'stats.statsView.activeDaysShort', { n: n0(cur.activeDays) })}
          hint={t('stats.statsView.promptsHint')}
        />
        <Tile
          label={t('stats.statsView.agentWork')}
          value={hours(cur.turnHours)}
          delta={comparable ? change(cur.turnHours, before.turnHours) : null}
          sub={
            cur.prompts
              ? t('stats.statsView.minPerPrompt', { n: fixed((cur.turnHours * 60) / cur.prompts, 1) })
              : undefined
          }
          hint={t('stats.statsView.agentWorkHint')}
        />
        <Tile
          label={t('stats.statsView.sessions')}
          value={n0(sessionsIn(data, filters, w).length)}
          delta={null}
          sub={cur.prs ? plural(cur.prs, 'stats.statsView.mrsLinked', { n: n0(cur.prs) }) : undefined}
          hint={t('stats.statsView.sessionsHint')}
        />
        <Tile
          label={t('stats.statsView.apiCost')}
          value={money(cur.cost)}
          delta={comparable ? change(cur.cost, before.cost) : null}
          inverse
          sub={
            cur.prompts ? t('stats.statsView.perPrompt', { value: money(cur.cost / cur.prompts) }) : undefined
          }
          hint={t('stats.statsView.apiCostHint')}
        />
        <Tile
          label={t('stats.statsView.cacheHit')}
          value={pct(hit, 1)}
          delta={comparable ? (hit !== null && hitBefore !== null ? hit - hitBefore : null) : null}
          sub={t('stats.statsView.fromCache', { value: tokens(cur.cacheRead) })}
          hint={t('stats.statsView.cacheHitHint')}
        />
        <Tile
          label={t('stats.statsView.errorsSeries')}
          value={pct(errRate, 1)}
          delta={comparable ? (errRate !== null && errBefore !== null ? errRate - errBefore : null) : null}
          inverse
          sub={`${plural(cur.interrupts, 'stats.statsView.interrupts', { n: n0(cur.interrupts) })} · ${plural(cur.denials, 'stats.statsView.denials', { n: n0(cur.denials) })}`}
          hint={t('stats.statsView.errorsHint')}
        />
      </div>

      <div className="ts-grid">
        <Panel
          span={12}
          title={t('stats.statsView.activityCalendar')}
          sub={t('stats.statsView.calendarSub', {
            prompts: plural(yearPrompts, 'stats.statsView.promptsThisYear', { n: n0(yearPrompts) }),
            days: plural(yearActiveDays, 'stats.statsView.activeDays'),
            current: plural(streak.current, 'stats.statsView.daysShort'),
            best: plural(streak.best, 'stats.statsView.daysShort'),
          })}
          hint={t('stats.statsView.calendarHint')}
        >
          <EChart option={calendarOpt} height={150} ariaLabel={t('stats.statsView.calendarAria')} />
          <div className="ts-legend-ramp" aria-hidden="true">
            <span>{t('stats.statsView.less')}</span>
            {th.ramp.map((c) => (
              <i key={c} style={{ background: c }} />
            ))}
            <span>{t('stats.statsView.more')}</span>
          </div>
        </Panel>

        <Panel
          span={8}
          title={t('stats.statsView.whoPrompts')}
          sub={t('stats.statsView.promptsByDay', { period: periodLabel })}
        >
          {noPrompts ? (
            <Empty height={300} />
          ) : (
            <EChart option={promptsOption} height={300} ariaLabel={t('stats.statsView.promptsByDayAria')} />
          )}
        </Panel>
        <Panel
          span={4}
          title={t('stats.statsView.team')}
          sub={periodLabel}
          hint={t('stats.statsView.teamHint')}
        >
          <ol className="ts-board">
            {board.map((row) => (
              <li key={row.person}>
                <div className="ts-board-top">
                  <span className="ts-board-name">
                    <span className="ts-dot" style={{ background: personColor(row.person) }} />
                    {personName(row.person)}
                  </span>
                  <strong>{n0(row.prompts)}</strong>
                </div>
                <div className="ts-meter">
                  <span
                    style={{
                      width: `${(row.prompts / boardMax) * 100}%`,
                      background: personColor(row.person),
                    }}
                  />
                </div>
                <div className="ts-board-meta">
                  <span>{plural(row.activeDays, 'stats.statsView.daysShort')}</span>
                  <span>{t('stats.statsView.agentHours', { hours: hours(row.turnHours) })}</span>
                  <span>{plural(row.sessions, 'stats.statsView.sessionsCount')}</span>
                </div>
                <div className="ts-board-meta">
                  <span>{money(row.cost)}</span>
                  <span>{t('stats.statsView.output', { value: tokens(row.output) })}</span>
                  <span>{t('stats.statsView.cache', { value: pct(cacheHitRate(row), 1) })}</span>
                </div>
              </li>
            ))}
            {board.length === 0 && <li className="ts-muted">{t('stats.statsView.noData')}</li>}
          </ol>
        </Panel>

        <Panel
          span={8}
          title={costBy === 'model' ? t('stats.statsView.costByModel') : t('stats.statsView.costByPerson')}
          sub={t('stats.statsView.atApiPrices', { period: periodLabel })}
          hint={t('stats.statsView.costHint')}
        >
          <div className="ts-table-actions">
            <Segmented
              label={t('stats.statsView.costSplit')}
              value={costBy}
              options={[
                { value: 'model', label: t('stats.statsView.byModel') },
                { value: 'person', label: t('stats.statsView.byPerson') },
              ]}
              onChange={setCostBy}
            />
          </div>
          {costSeries.length === 0 || cur.cost === 0 ? (
            <Empty height={300} />
          ) : (
            <EChart
              option={costOption}
              height={300}
              ariaLabel={
                costBy === 'model'
                  ? t('stats.statsView.costByModelAria')
                  : t('stats.statsView.costByPersonAria')
              }
            />
          )}
        </Panel>
        <Panel
          span={4}
          title={t('stats.statsView.tokens')}
          sub={periodLabel}
          hint={t('stats.statsView.tokensHint')}
        >
          <dl className="ts-kv">
            <div>
              <dt>{t('stats.statsView.modelReplies')}</dt>
              <dd>{n0(cur.calls)}</dd>
            </div>
            <div>
              <dt>{t('stats.statsView.input')}</dt>
              <dd>{tokens(cur.input)}</dd>
            </div>
            <div>
              <dt>{t('stats.statsView.outputLabel')}</dt>
              <dd>{tokens(cur.output)}</dd>
            </div>
            <div>
              <dt>{t('stats.statsView.cacheWrite')}</dt>
              <dd>{tokens(cur.cacheWrite)}</dd>
            </div>
            <div>
              <dt>{t('stats.statsView.cacheRead')}</dt>
              <dd>{tokens(cur.cacheRead)}</dd>
            </div>
            <div>
              <dt>{t('stats.statsView.outputPerPrompt')}</dt>
              <dd>{cur.prompts ? tokens(cur.output / cur.prompts) : '—'}</dd>
            </div>
          </dl>
          {board.length > 0 && (
            <table className="ts-table ts-mini">
              <thead>
                <tr>
                  <th>{t('stats.statsView.byPerson')}</th>
                  <th className="ts-num">{t('stats.statsView.outputLabel')}</th>
                  <th className="ts-num">{t('stats.statsView.fromCacheCol')}</th>
                  <th className="ts-num">{t('stats.statsView.cost')}</th>
                </tr>
              </thead>
              <tbody>
                {board.map((row) => (
                  <tr key={row.person}>
                    <td>
                      <span className="ts-person">
                        <span className="ts-dot" style={{ background: personColor(row.person) }} />
                        {personName(row.person)}
                      </span>
                    </td>
                    <td className="ts-num">{tokens(row.output)}</td>
                    <td className="ts-num">{tokens(row.cacheRead)}</td>
                    <td className="ts-num">{money(row.cost)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Panel>

        <Panel span={6} title={t('stats.statsView.whenWeWork')} sub={t('stats.statsView.punchSub')}>
          {noPrompts ? (
            <Empty height={230} />
          ) : (
            <EChart option={punchOption} height={230} ariaLabel={t('stats.statsView.punchAria')} />
          )}
        </Panel>
        <Panel
          span={6}
          title={t('stats.statsView.projects')}
          sub={t('stats.statsView.costByRepo', { period: periodLabel })}
          hint={t('stats.statsView.projectsHint')}
        >
          {projects.length === 0 ? (
            <Empty height={230} />
          ) : (
            <EChart
              option={projectsOption}
              height={Math.max(160, projects.length * 26)}
              ariaLabel={t('stats.statsView.costByProjectAria')}
            />
          )}
        </Panel>

        <Panel
          span={12}
          title={t('stats.statsView.agentWork')}
          sub={t('stats.statsView.hoursByDay', { period: periodLabel })}
          hint={t('stats.statsView.hoursHint')}
        >
          {cur.turnHours === 0 ? (
            <Empty height={240} />
          ) : (
            <EChart option={hoursOption} height={240} ariaLabel={t('stats.statsView.hoursAria')} />
          )}
        </Panel>

        <Panel
          span={6}
          title={t('stats.statsView.cacheHit')}
          sub={t('stats.statsView.perDay')}
          hint={t('stats.statsView.cacheHint')}
        >
          {cacheByDay.every((v) => v === null) ? (
            <Empty height={200} />
          ) : (
            <EChart option={cacheOption} height={200} ariaLabel={t('stats.statsView.cacheAria')} />
          )}
        </Panel>
        <Panel
          span={6}
          title={t('stats.statsView.errorsSeries')}
          sub={t('stats.statsView.errorsSub')}
          hint={t('stats.statsView.errorsPanelHint')}
        >
          {quality.errors.every((v) => v === null) ? (
            <Empty height={200} />
          ) : (
            <EChart option={errorsOption} height={200} ariaLabel={t('stats.statsView.errorsAria')} />
          )}
        </Panel>

        <Panel
          span={12}
          title={t('stats.statsView.tasks')}
          sub={t('stats.statsView.tasksSub')}
          hint={t('stats.statsView.tasksHint')}
        >
          {tasks.length === 0 ? (
            <Empty height={80} text={t('stats.statsView.noTasks')} />
          ) : (
            <div className="ts-table-wrap">
              <table className="ts-table">
                <thead>
                  <tr>
                    <th>{t('stats.statsView.task')}</th>
                    <th>{t('stats.statsView.who')}</th>
                    <th className="ts-num">{t('stats.statsView.sessionsCol')}</th>
                    <th className="ts-num">{t('stats.statsView.promptsCol')}</th>
                    <th className="ts-num">{t('stats.statsView.agentWorkCol')}</th>
                    <th className="ts-num">{t('stats.statsView.cost')}</th>
                  </tr>
                </thead>
                <tbody>
                  {tasks.map((row) => (
                    <tr key={row.task}>
                      <td>
                        <a
                          href={issueUrlOf(row.task) ?? undefined}
                          target="_blank"
                          rel="noreferrer"
                          className="ts-link"
                        >
                          {row.task}
                        </a>
                      </td>
                      <td>
                        {row.people.map((p) => (
                          <span key={p} className="ts-person">
                            <span className="ts-dot" style={{ background: personColor(p) }} />
                            {personName(p)}
                          </span>
                        ))}
                      </td>
                      <td className="ts-num">{n0(row.sessions)}</td>
                      <td className="ts-num">{n0(row.prompts)}</td>
                      <td className="ts-num">{hours(row.turnHours)}</td>
                      <td className="ts-num">{money(row.cost)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel
          span={12}
          title={t('stats.statsView.sessions')}
          sub={t('stats.statsView.sessionsSub')}
          hint={t('stats.statsView.sessionsPanelHint')}
        >
          <div className="ts-table-actions">
            <Segmented
              label={t('stats.statsView.sorting')}
              value={sessionSort}
              options={[
                { value: 'cost', label: t('stats.statsView.byCost') },
                { value: 'prompts', label: t('stats.statsView.byPrompts') },
                { value: 'end', label: t('stats.statsView.recent') },
              ]}
              onChange={setSessionSort}
            />
          </div>
          {topSessions.length === 0 ? (
            <Empty height={80} />
          ) : (
            <div className="ts-table-wrap">
              <table className="ts-table">
                <thead>
                  <tr>
                    <th>{t('stats.statsView.session')}</th>
                    <th>{t('stats.statsView.who')}</th>
                    <th>{t('stats.statsView.project')}</th>
                    <th>{t('stats.statsView.model')}</th>
                    <th className="ts-num">{t('stats.statsView.promptsCol')}</th>
                    <th className="ts-num">{t('stats.statsView.agentWorkOne')}</th>
                    <th className="ts-num">{t('stats.statsView.cost')}</th>
                  </tr>
                </thead>
                <tbody>
                  {topSessions.map((s: IPersonSession) => (
                    <tr key={`${s.person}:${s.id}`}>
                      <td className="ts-session">
                        {s.title ? (
                          <span title={s.title}>{s.title}</span>
                        ) : (
                          <span className="ts-muted">
                            {t('stats.statsView.sessionOf', { person: personName(s.person) })}
                          </span>
                        )}
                        <small>
                          {new Date(s.end * 1000).toLocaleString(LOCALE_TAG, {
                            day: 'numeric',
                            month: 'short',
                            hour: '2-digit',
                            minute: '2-digit',
                          })}{' '}
                          · {s.machineName}
                          {s.tasks.length ? ` · ${s.tasks.slice(0, 3).join(', ')}` : ''}
                          {s.subagentCost > 0
                            ? t('stats.statsView.subagentsShare', { value: pct(s.subagentCost / s.cost) })
                            : ''}
                        </small>
                      </td>
                      <td>
                        <span className="ts-person">
                          <span className="ts-dot" style={{ background: personColor(s.person) }} />
                          {personName(s.person)}
                        </span>
                      </td>
                      <td>{s.project ?? '—'}</td>
                      <td>{s.model ? modelName(s.model) : '—'}</td>
                      <td className="ts-num">{n0(s.prompts)}</td>
                      <td className="ts-num">{hours(s.turnMs / 3_600_000)}</td>
                      <td className="ts-num">{money(s.cost)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      </div>
    </div>
  );
};

export default StatsView;
