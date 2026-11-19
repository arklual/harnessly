// The «Аналитика» screen of the board «Хаб». Its own chunk: it loads when the person opens the section.
// The period, the comparison, the chosen metric and the way the chart is drawn live in the address, so a link shows what
// its sender saw. The numbers are asked by the period; the old ones stay (dimmed) until the new ones come.
import { useCallback, useEffect, useMemo, useState } from 'react';

import type { AnalyticsData, AnalyticsFrame, AnalyticsQuery, KpiId } from '../../org/api/analytics';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { navigate, useLocation } from '../../lib/router';
import { Btn } from '../ui';
import { liveOn } from '../flag';
import { Head, Titles } from '../shell';
import { money, plural } from './analytics/calc';
import type { ChartMode } from './analytics/chart';
import { SpendCard, SubsCard, WhereCard } from './analytics/spend';
import { PeriodControls } from './analytics/header';
import { Hero } from './analytics/hero';
import { decodePeriod, encodePeriod, periodNameOf, type PickerState, queryOf } from './analytics/period';
import { chartKey, createStore, dataKey, useAnswer } from './analytics/store';
import {
  AttentionCard,
  ChangesCard,
  HeatCard,
  kpiCell,
  Strip,
  type StripCell,
  WasteRows,
} from './analytics/macro';

const KPI_IDS: readonly KpiId[] = [
  'moneyPerMr',
  'yield',
  'timeToMr',
  'mrPerPerson',
  'autonomy',
  'waiting',
  'parallel',
  'toolErrors',
  'reread',
  'sessionStart',
  'cache',
  'skills',
  'cacheWrite1h',
  'mrSurvival',
  'waitingP90',
];
const FIRST_MODE: ChartMode = 'teams';

/** The sub-tabs of the board (`ASEC`): the address carries `s`, the first one has none. */
const SECTIONS = [
  ['home', 'Главная'],
  ['money', 'Деньги'],
  ['res', 'Результат'],
  ['agent', 'Работа агента'],
  ['env', 'Контекст и кэш'],
] as const;
type Section = (typeof SECTIONS)[number][0];
const sectionOf = (s: string | null): Section => SECTIONS.find(([id]) => id === s)?.[0] ?? 'home';

/** The big charts of each sub-tab, in the board's order (the ones the hub draws). */
const CHARTS: Record<Section, readonly KpiId[]> = {
  home: ['moneyPerMr'],
  money: [],
  res: ['moneyPerMr', 'timeToMr', 'mrSurvival'],
  agent: ['autonomy', 'waitingP90', 'toolErrors', 'parallel'],
  env: ['sessionStart', 'cache', 'reread', 'skills'],
};
/** The strip of figures of each sub-tab; `spend`, `mrs` and `waste` come from the totals, the rest are metrics. */
type StripId = KpiId | 'spend' | 'mrs' | 'waste' | 'forecast';
const STRIP: Record<Section, readonly StripId[]> = {
  home: ['spend', 'moneyPerMr', 'mrs', 'yield', 'autonomy', 'waste'],
  money: ['spend', 'forecast', 'waste'],
  res: ['moneyPerMr', 'mrs', 'yield', 'timeToMr', 'mrPerPerson', 'mrSurvival'],
  agent: ['autonomy', 'waiting', 'parallel', 'toolErrors', 'waitingP90'],
  env: ['sessionStart', 'cache', 'cacheWrite1h', 'reread', 'skills'],
};

interface Picked {
  picker: PickerState;
  /** The metric opened on a chart of its own (a click on a figure, a link of a signal); null: only the sub-tab's charts. */
  kpi: KpiId | null;
  mode: ChartMode;
}

const fromUrl = (frame: AnalyticsFrame, staff: boolean): Picked => {
  const q = new URLSearchParams(location.search);
  const k = q.get('k');
  return {
    picker: decodePeriod(frame.weeks, { p: q.get('p'), c: q.get('c') }, frame.defaultPeriod),
    kpi: KPI_IDS.find((id) => id === k) ?? null,
    mode: staff && q.get('m') !== 'cmp' ? FIRST_MODE : 'cmp',
  };
};

/** Writes the choice into the address without a new history entry and without waking the router. */
const toUrl = (frame: AnalyticsFrame, p: Picked, staff: boolean): void => {
  const q = new URLSearchParams(location.search);
  const set = (key: string, v: string | null) => (v === null ? q.delete(key) : q.set(key, v));
  const per = encodePeriod(frame.weeks, p.picker);
  set('p', per.p);
  set('c', per.c);
  set('k', p.kpi);
  set('m', staff && p.mode !== FIRST_MODE ? p.mode : null);
  const s = q.toString();
  const next = `${location.pathname}${s ? `?${s}` : ''}${location.hash}`;
  if (next !== `${location.pathname}${location.search}${location.hash}`)
    history.replaceState(history.state, '', next);
};

const Skeleton = () => (
  <div className="an-sk" aria-hidden="true">
    <div className="an-sk__tiles">
      {Array.from({ length: 8 }, (_, i) => (
        <div key={i} className="hx-sk an-sk__tile" />
      ))}
    </div>
    <div className="hx-sk an-sk__chart" />
  </div>
);

const Failure = ({ title, error, retry }: { title: string; error: unknown; retry: () => void }) => (
  <p className="an-err" role="alert">
    <b>{title}</b>
    <span>{orgErrorText(error)}</span>
    <Btn size="sm" icon="rotate-cw" onClick={retry}>
      Повторить
    </Btn>
  </p>
);

/** The metrics the «Эффективность» screen opens with their own chart. */
const EFFICIENCY_KPIS: readonly KpiId[] = [
  'reread',
  'sessionStart',
  'cache',
  'cacheWrite1h',
  'waiting',
  'waitingP90',
];

/** One big chart of the board: it asks for its own series and draws it under the metric's name. */
const ChartBlock = ({
  id,
  data,
  query,
  store,
  staff,
  mode,
  onMode,
  teamOrder,
  marker,
}: {
  id: KpiId;
  data: AnalyticsData;
  query: AnalyticsQuery;
  store: ReturnType<typeof createStore>;
  staff: boolean;
  mode: ChartMode;
  onMode: (m: ChartMode) => void;
  teamOrder: readonly string[];
  marker?: { value: string | null; set: (id: string | null) => void };
}) => {
  const key = chartKey(query, id);
  const chart = useAnswer(
    key,
    () => store.chart(query, id),
    () => store.forget(key),
  );
  const kpi = data.kpis.find((k) => k.id === id);
  if (!kpi) return null;
  const shown = chart.data ? (data.kpis.find((k) => k.id === chart.data?.kpi) ?? null) : null;
  return (
    <div data-hx-chart={id}>
      <Hero
        kpi={kpi}
        chart={chart.data}
        fresh={chart.fresh}
        error={chart.error}
        retry={chart.reload}
        staff={staff}
        mode={mode}
        onMode={onMode}
        shownKpi={shown}
        teamOrder={teamOrder}
        {...(marker ? { marker: marker.value, onMarker: marker.set } : {})}
      />
    </div>
  );
};

/** «+12%» against the period before: for money a rise is bad, for merged work it is good. */
const changeOf = (now: number, was: number | null | undefined, upGood: boolean): StripCell['delta'] => {
  if (typeof was !== 'number' || was <= 0) return null;
  const pct = ((now - was) / was) * 100;
  if (Math.abs(pct) < 0.5) return { text: 'как раньше', tone: 'flat' };
  const up = pct > 0;
  return { text: `${up ? '+' : '−'}${Math.round(Math.abs(pct))}%`, tone: up === upGood ? 'good' : 'bad' };
};

const stripCells = (ids: readonly StripId[], d: AnalyticsData): StripCell[] => {
  const out: StripCell[] = [];
  for (const id of ids) {
    if (id === 'spend')
      out.push({
        id,
        label: 'Расход',
        value: money(d.summary.spendUsd),
        delta: changeOf(d.summary.spendUsd, d.summary.prev?.spendUsd, false),
        sub: `${d.summary.sessions} ${plural(d.summary.sessions, 'сессия', 'сессии', 'сессий')}`,
        hint: 'Сколько ушло на агентов за период.',
        go: null,
      });
    else if (id === 'mrs')
      out.push({
        id,
        label: 'Смёрженных MR',
        value: String(d.summary.mrs),
        delta: changeOf(d.summary.mrs, d.summary.prev?.mrs, true),
        sub: null,
        hint: 'MR, сделанные с агентом и влитые в основную ветку за период.',
        go: 'mrPerPerson',
      });
    else if (id === 'forecast') {
      const f = d.summary.forecastUsd;
      if (typeof f !== 'number') continue;
      const b = d.summary.budgetUsd;
      out.push({
        id,
        label: 'Прогноз на месяц',
        value: money(f),
        delta: null,
        sub: typeof b === 'number' && b > 0 ? `бюджет ${money(b)}` : null,
        hint: 'Сколько выйдет за месяц, если расход пойдёт теми же темпами.',
        go: null,
      });
    } else if (id === 'waste') {
      if (!d.waste) continue;
      const share = d.waste.totalUsd > 0 ? Math.round((d.waste.wasteUsd / d.waste.totalUsd) * 100) : 0;
      out.push({
        id,
        label: 'Ушло впустую',
        value: `${share}%`,
        delta: null,
        sub: money(d.waste.wasteUsd),
        hint: 'Доля расхода, которая не дала результата.',
        go: null,
      });
    } else {
      const k = d.kpis.find((x) => x.id === id);
      if (k) out.push(kpiCell(k));
    }
  }
  return out;
};

const Body = ({ frame }: { frame: AnalyticsFrame }) => {
  const { api, shell, base } = useOrg();
  const live = liveOn(shell.features);
  // The change pinned on the big charts; the list of changes pins it too.
  const [pinned, setPinned] = useState<string | null>(null);
  const staff = shell.viewer.role !== 'member';
  const store = useMemo(() => createStore(api), [api]);
  const [picked, setPicked] = useState<Picked>(() => fromUrl(frame, staff));
  // A link into the open screen (a signal's «Смотреть», a sub-tab of the menu) changes the address with a new entry.
  const { search } = useLocation();
  const [seenSearch, setSeenSearch] = useState(search);
  if (search !== seenSearch) {
    setSeenSearch(search);
    const k = new URLSearchParams(search).get('k');
    const id = KPI_IDS.find((x) => x === k);
    if (id && id !== picked.kpi) setPicked((p) => ({ ...p, kpi: id }));
  }
  const sec = sectionOf(new URLSearchParams(search).get('s'));
  const { picker, kpi: kpiId, mode } = picked;
  const teamOrder = useMemo(() => shell.teams.map((t) => t.name), [shell.teams]);

  useEffect(() => toUrl(frame, picked, staff), [frame, picked, staff]);

  const query = useMemo(
    () => queryOf(frame.weeks, picker),
    // The ranges decide the query; a click that only moves the open tab does not ask again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [frame.weeks, picker.a, picker.b, picker.free],
  );
  const dKey = dataKey(query);
  const data = useAnswer(
    dKey,
    () => store.data(query),
    () => store.forget(dKey),
  );

  const onPicker = useCallback((next: PickerState) => setPicked((p) => ({ ...p, picker: next })), []);
  const onMode = useCallback((m: ChartMode) => setPicked((p) => ({ ...p, mode: m })), []);
  // A click on a figure opens the chart of its metric: on the sub-tab's own chart, or one added at the end.
  const onGo = useCallback((id: KpiId) => {
    setPicked((p) => ({ ...p, kpi: id }));
    window.setTimeout(() => {
      document
        .querySelector(`[data-hx-chart="${id}"]`)
        ?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }, 160);
  }, []);
  const goSection = (to: Section) => {
    const q = new URLSearchParams(location.search);
    if (to === 'home') q.delete('s');
    else q.set('s', to);
    const qs = q.toString();
    navigate(`${location.pathname}${qs ? `?${qs}` : ''}`);
  };

  const nowName = periodNameOf(frame.weeks, picker);
  const d = data.data;
  const lead = d
    ? `${d.scopeLabel} · ${nowName.dates}: ${
        data.fresh
          ? `${money(d.summary.spendUsd)} · ${d.summary.sessions} ${plural(d.summary.sessions, 'сессия', 'сессии', 'сессий')} · ${d.summary.mrs} MR`
          : 'считаем'
      }`
    : 'Считаем';
  const label = SECTIONS.find(([id]) => id === sec)?.[1] ?? '';
  const title =
    sec === 'home' ? (
      'Аналитика'
    ) : (
      <>
        <span className="an-crumb" role="button" tabIndex={0} onClick={() => goSection('home')}>
          Аналитика
        </span>
        <span className="an-crumb__s">/</span>
        {label}
      </>
    );

  const marker = live ? { value: pinned, set: setPinned } : undefined;
  const chartProps = { query, store, staff, mode, onMode, teamOrder, marker };
  const extra = kpiId && !CHARTS[sec].includes(kpiId) ? kpiId : null;
  const pickChange = live
    ? (id: string) => {
        // The charts show only the changes of their days: one out of them stays a row to read.
        const next = pinned === id ? null : id;
        setPinned(next);
        if (next)
          document
            .querySelector('[data-hx-part="chart"]')
            ?.scrollIntoView({ behavior: 'smooth', block: 'center' });
      }
    : undefined;
  const allChanges = () => navigate(`${base}/efficiency`);
  const changes = d && (
    <ChangesCard
      changes={d.changes}
      selected={pinned}
      {...(pickChange ? { onPick: pickChange } : {})}
      onAll={allChanges}
    />
  );
  const charts = (ids: readonly KpiId[]) =>
    d && ids.map((id) => <ChartBlock key={id} id={id} data={d} {...chartProps} />);

  return (
    <>
      <Head
        title={title}
        lead={lead}
        acts={
          <PeriodControls
            weeks={frame.weeks}
            change={frame.lastChange}
            state={picker}
            onChange={onPicker}
            compact
          />
        }
      />
      <div className="dash hb-dash anx" data-busy={d && !data.fresh && !data.error ? '1' : undefined}>
        {data.error && !data.fresh ? (
          <Failure title="Не удалось загрузить аналитику" error={data.error} retry={data.reload} />
        ) : !d ? (
          <Skeleton />
        ) : (
          <div className="an-stage">
            <div className="mx">
              <Strip cells={stripCells(STRIP[sec], d)} onGo={onGo} />
              {sec === 'home' && (
                <>
                  {staff && <AttentionCard signals={d.signals} base={base} />}
                  {changes}
                  {charts(CHARTS.home)}
                  {staff && d.peers && <HeatCard peers={d.peers} kpis={d.kpis} onGo={onGo} />}
                </>
              )}
              {sec === 'money' && (
                <>
                  <SpendCard query={query} staff={staff} />
                  <WhereCard query={query} staff={staff} />
                  {staff && d.waste ? <WasteRows waste={d.waste} /> : null}
                  <SubsCard query={query} />
                </>
              )}
              {sec === 'res' && (
                <>
                  {charts(CHARTS.res)}
                  {staff && d.peers && <HeatCard peers={d.peers} kpis={d.kpis} onGo={onGo} />}
                </>
              )}
              {sec === 'agent' && charts(CHARTS.agent)}
              {sec === 'env' && (
                <>
                  {charts(CHARTS.env)}
                  {changes}
                </>
              )}
              {extra && charts([extra])}
              {extra && live && EFFICIENCY_KPIS.includes(extra) && (
                <div className="anx-what">
                  <Btn
                    size="sm"
                    icon="gauge"
                    data-variant="outline"
                    onClick={() => navigate(`${base}/efficiency?k=${extra}`)}
                  >
                    Разрез на экране «Эффективность»
                  </Btn>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </>
  );
};

const Screen = () => {
  const { api, base } = useOrg();
  const frame = useAnswer(
    base,
    () => api.analyticsFrame(),
    () => undefined,
  );
  if (frame.data) return <Body key={base} frame={frame.data} />;
  return (
    <>
      <div className="hb-head hd-head">
        <Titles title="Аналитика" lead="Считаем" />
      </div>
      <div className="dash hb-dash anx">
        {frame.error ? (
          <Failure title="Не удалось загрузить аналитику" error={frame.error} retry={frame.reload} />
        ) : (
          <Skeleton />
        )}
      </div>
    </>
  );
};

export default Screen;
