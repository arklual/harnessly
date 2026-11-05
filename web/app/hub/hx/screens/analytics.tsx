// The «Аналитика» screen of the board «Хаб». Its own chunk: it loads when the person opens the section.
// The period, the comparison, the chosen metric and the way the chart is drawn live in the address, so a link shows what
// its sender saw. The numbers are asked by the period; the old ones stay (dimmed) until the new ones come.
import { useCallback, useEffect, useMemo, useState } from 'react';

import type { AnalyticsFrame, KpiId } from '../../org/api/analytics';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { useLocation } from '../../lib/router';
import { Btn } from '../ui';
import { Titles } from '../shell';
import { Attention, Changes, PeersCard, WasteCard } from './analytics/cards';
import { totalsText } from './analytics/calc';
import type { ChartMode } from './analytics/chart';
import { PeriodControls } from './analytics/header';
import { Hero } from './analytics/hero';
import {
  compareNameOf,
  decodePeriod,
  encodePeriod,
  nameOf,
  type PickerState,
  queryOf,
} from './analytics/period';
import { chartKey, createStore, dataKey, useAnswer } from './analytics/store';
import { Tiles } from './analytics/tiles';

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
];
const FIRST_KPI: KpiId = 'moneyPerMr';
const FIRST_MODE: ChartMode = 'teams';

interface Picked {
  picker: PickerState;
  kpi: KpiId;
  mode: ChartMode;
}

const fromUrl = (frame: AnalyticsFrame, staff: boolean): Picked => {
  const q = new URLSearchParams(location.search);
  const k = q.get('k');
  return {
    picker: decodePeriod(frame.weeks, { p: q.get('p'), c: q.get('c') }, frame.defaultPeriod),
    kpi: KPI_IDS.find((id) => id === k) ?? FIRST_KPI,
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
  set('k', p.kpi === FIRST_KPI ? null : p.kpi);
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

const Body = ({ frame }: { frame: AnalyticsFrame }) => {
  const { api, shell, base } = useOrg();
  const staff = shell.viewer.role !== 'member';
  const store = useMemo(() => createStore(api), [api]);
  const [picked, setPicked] = useState<Picked>(() => fromUrl(frame, staff));
  // A link into the open screen (a signal's «Смотреть») changes the address with a new entry: take its KPI.
  const { search } = useLocation();
  const [seenSearch, setSeenSearch] = useState(search);
  if (search !== seenSearch) {
    setSeenSearch(search);
    const k = new URLSearchParams(search).get('k');
    const id = KPI_IDS.find((x) => x === k);
    if (id && id !== picked.kpi) setPicked((p) => ({ ...p, kpi: id }));
  }
  const { picker, kpi: kpiId, mode } = picked;
  const teamOrder = useMemo(() => shell.teams.map((t) => t.name), [shell.teams]);

  useEffect(() => toUrl(frame, picked, staff), [frame, picked, staff]);

  const query = useMemo(
    () => queryOf(frame.weeks, picker),
    // The ranges decide the query; a click that only moves the open tab does not ask again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [frame.weeks, picker.a, picker.b],
  );
  const dKey = dataKey(query);
  const data = useAnswer(
    dKey,
    () => store.data(query),
    () => store.forget(dKey),
  );
  const cKey = chartKey(query, kpiId);
  const chart = useAnswer(
    cKey,
    () => store.chart(query, kpiId),
    () => store.forget(cKey),
  );

  const onPicker = useCallback((next: PickerState) => setPicked((p) => ({ ...p, picker: next })), []);
  const onKpi = useCallback((id: KpiId) => setPicked((p) => ({ ...p, kpi: id })), []);
  const onMode = useCallback((m: ChartMode) => setPicked((p) => ({ ...p, mode: m })), []);
  // The series is fetched while the pointer is still on the tile, so the click finds it ready.
  const warm = useCallback((id: KpiId) => void store.chart(query, id).catch(() => undefined), [store, query]);

  const nowName = nameOf(frame.weeks, picker.a, { month: true });
  const prevName = compareNameOf(frame.weeks, picker);
  const nowLabel = `${nowName.title.toLowerCase()} (${nowName.dates})`;
  const prevLabel = `${prevName.title.toLowerCase()} (${prevName.dates})`;
  const d = data.data;
  const kpi = d?.kpis.find((k) => k.id === kpiId) ?? null;
  const shownKpi = chart.data ? (d?.kpis.find((k) => k.id === chart.data?.kpi) ?? null) : null;
  const lead = d
    ? `${d.scopeLabel} · за ${nowLabel}: ${data.fresh ? totalsText(d.summary) : 'считаем'}`
    : 'Считаем';

  return (
    <>
      <div className="ad-head">
        <Titles title="Аналитика" lead={lead} />
        <PeriodControls weeks={frame.weeks} change={frame.lastChange} state={picker} onChange={onPicker} />
      </div>
      <div className="dash ad-dash anx" data-busy={d && !data.fresh && !data.error ? '1' : undefined}>
        {data.error && !data.fresh ? (
          <Failure title="Не удалось загрузить аналитику" error={data.error} retry={data.reload} />
        ) : !d ? (
          <Skeleton />
        ) : (
          <div className="an-stage">
            <div className="mx">
              <Tiles
                kpis={d.kpis}
                selected={kpiId}
                nowLabel={nowLabel}
                prevLabel={prevLabel}
                onPick={onKpi}
                onHover={warm}
              />
              {kpi && (
                <Hero
                  kpi={kpi}
                  chart={chart.data}
                  fresh={chart.fresh}
                  error={chart.error}
                  retry={chart.reload}
                  staff={staff}
                  mode={mode}
                  onMode={onMode}
                  shownKpi={shownKpi}
                  teamOrder={teamOrder}
                />
              )}
              <Changes changes={d.changes} />
              {staff && <Attention signals={d.signals} base={base} />}
              {staff && d.waste && <WasteCard waste={d.waste} />}
              {staff && d.peers && <PeersCard peers={d.peers} teamOrder={teamOrder} />}
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
      <div className="ad-head">
        <Titles title="Аналитика" lead="Считаем" />
      </div>
      <div className="dash ad-dash anx">
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
