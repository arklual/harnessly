// The «Эффективность» screen: the price of a merged MR, what moves it, where the money is lost, what changed
// and what is worth switching on. One screen for the scale the viewer's address names (a person, a team, the organization).
// It is built on what the hub's analytics already tells; what it does not have yet (the split by stream, a limit window
// before the hub began to keep cycles) is said on the screen, never drawn from nothing. Comes with the lively hub (`ui.hx.live`).
import { useCallback, useEffect, useMemo, useState } from 'react';

import type { AnalyticsFrame, KpiId, LimitHistory } from '../../org/api/analytics';
import { orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { navigate } from '../../lib/router';
import { Count } from '../live';
import { useParams } from '../sort';
import { Titles } from '../shell';
import { Btn } from '../ui';
import { Card, Changes, PeersCard } from './analytics/cards';
import { deltaOf, formatKpi, money, plural, wasText } from './analytics/calc';
import type { ChartMode } from './analytics/chart';
import { PeriodControls } from './analytics/header';
import { Hero } from './analytics/hero';
import {
  compareNameOf,
  decodePeriod,
  encodePeriod,
  periodNameOf,
  type PickerState,
  queryOf,
} from './analytics/period';
import { chartKey, createStore, dataKey, useAnswer } from './analytics/store';
import { Tile } from './analytics/tiles';
import '../styles/efficiency.css';

/** The metrics under the price, as the spec names them: what makes a merged MR expensive. */
const DRIVERS: readonly KpiId[] = [
  'reread',
  'sessionStart',
  'cache',
  'cacheWrite1h',
  'waiting',
  'waitingP90',
];
/** Fewer merged MRs than this and the price is not told. */
const MIN_MRS = 5;

type Tab = 'price' | 'mrs' | 'waste' | 'limit';
const TABS: readonly Tab[] = ['price', 'mrs', 'waste', 'limit'];

const WEEK = 604_800;
const dayText = (iso: string): string => iso.slice(8, 10) + '.' + iso.slice(5, 7);

/** The weekly windows of the accounts: the base of the «Лимит недели» tab. */
const weekly = (h: LimitHistory) =>
  (h ?? []).flatMap((a) =>
    a.windows.filter((w) => w.lengthSec >= WEEK && w.cycles.length > 0).map((w) => ({ a, w })),
  );

const avgPeak = (h: LimitHistory): number | null => {
  const rows = weekly(h);
  const cycles = rows.reduce((n, r) => n + r.w.cycles.length, 0);
  if (cycles === 0) return null;
  return rows.reduce((s, r) => s + r.w.cycles.reduce((x, c) => x + c.peakPct, 0), 0) / cycles;
};

const LimitHistoryView = ({ history }: { history: LimitHistory }) => {
  const rows = weekly(history);
  if (rows.length === 0)
    return (
      <p className="ef-note" role="status">
        <b>Данных пока нет</b>
        <span>
          Хаб начал хранить циклы лимита недавно: за более ранние недели их нет, поэтому прогноз не рисуем.
          Цикл появится после первого замера окна.
        </span>
      </p>
    );
  return (
    <Card title="Циклы лимита недели" lead="Пик расхода по неделям, до сброса: хватило ли подписки">
      <ol className="ef-levers">
        {rows.map(({ a, w }) => (
          <li key={`${a.id}-${w.name}`} className="ef-lever">
            <span className="ef-lever__t">
              <b>{a.label}</b>
              <span>
                Пик по неделям (сброс):{' '}
                {w.cycles.map((c) => `${dayText(c.endsAt)} — ${Math.round(c.peakPct)}%`).join(' · ')}
              </span>
            </span>
            <span className="ef-lever__v">
              в среднем {Math.round(w.avgPeakPct)}%
              {w.fullCycles > 0 ? `, упёрлись в лимит: ${w.fullCycles}` : ''}
            </span>
          </li>
        ))}
      </ol>
    </Card>
  );
};

const Skeleton = () => (
  <div className="ef-sk" aria-hidden="true">
    <div className="hx-sk ef-sk__main" />
    <div className="ef-sk__tiles">
      {Array.from({ length: 4 }, (_, i) => (
        <div key={i} className="hx-sk ef-sk__tile" />
      ))}
    </div>
    <div className="hx-sk ef-sk__card" />
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

const Scales = () => {
  const { shell, base } = useOrg();
  const parts = base.split('/');
  const orgBase = parts.slice(0, 3).join('/');
  const here = parts[3] ?? 'all';
  const role = shell.viewer.role;
  const items: { id: string; label: string; to: string | null }[] = [];
  if (role === 'member') items.push({ id: 'me', label: 'Я', to: null });
  else {
    const mine = shell.tenants[0];
    if (role === 'admin') items.push({ id: 'all', label: 'Организация', to: `${orgBase}/all/efficiency` });
    if (mine)
      items.push({ id: mine.slug, label: `Команда ${mine.name}`, to: `${orgBase}/${mine.slug}/efficiency` });
  }
  if (items.length < 2) return null;
  const on = (id: string) => (role === 'member' ? true : id === here || (here !== 'all' && id !== 'all'));
  return (
    <div className="hx-set__seg ef-scale" role="radiogroup" aria-label="Масштаб">
      {items.map((o) => (
        <button
          key={o.id}
          type="button"
          className="hx-set__seg-item"
          role="radio"
          aria-checked={on(o.id)}
          onClick={() => o.to && navigate(o.to)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
};

const Body = ({ frame }: { frame: AnalyticsFrame }) => {
  const { api, shell } = useOrg();
  const staff = shell.viewer.role !== 'member';
  const store = useMemo(() => createStore(api), [api]);
  const p = useParams();
  const [picker, setPicker] = useState<PickerState>(() => {
    const q = new URLSearchParams(location.search);
    return decodePeriod(frame.weeks, { p: q.get('p'), c: q.get('c') }, frame.defaultPeriod);
  });
  const tabParam = p.get('t');
  const tab: Tab = TABS.find((t) => t === tabParam) ?? 'price';
  const driverParam = p.get('k');
  const driver = DRIVERS.find((k) => k === driverParam) ?? null;
  const [mode, setMode] = useState<ChartMode>(staff ? 'teams' : 'cmp');
  const teamOrder = useMemo(() => shell.teams.map((t) => t.name), [shell.teams]);

  useEffect(() => {
    const per = encodePeriod(frame.weeks, picker);
    p.set({ p: per.p, c: per.c });
    // The period only: the tab and the driver write themselves.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [frame, picker]);

  const query = useMemo(
    () => queryOf(frame.weeks, picker),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [frame.weeks, picker.a, picker.b, picker.free],
  );
  const dKey = dataKey(query);
  const data = useAnswer(
    dKey,
    () => store.data(query),
    () => store.forget(dKey),
  );
  const lKey = `lim:${dKey}`;
  const limits = useAnswer(
    lKey,
    () => api.limitWindows(query),
    () => undefined,
  );
  const chartKpi: KpiId = driver ?? 'moneyPerMr';
  const cKey = chartKey(query, chartKpi);
  const chart = useAnswer(
    cKey,
    () => store.chart(query, chartKpi),
    () => store.forget(cKey),
  );
  const warm = useCallback((id: KpiId) => void store.chart(query, id).catch(() => undefined), [store, query]);

  const nowName = periodNameOf(frame.weeks, picker);
  const prevName = compareNameOf(frame.weeks, picker);
  const nowLabel = `${nowName.title.toLowerCase()} (${nowName.dates})`;
  const prevLabel = `${prevName.title.toLowerCase()} (${prevName.dates})`;
  const d = data.data;
  const price = d?.kpis.find((k) => k.id === 'moneyPerMr') ?? null;
  const picked = d?.kpis.find((k) => k.id === chartKpi) ?? null;
  const shownKpi = chart.data ? (d?.kpis.find((k) => k.id === chart.data?.kpi) ?? null) : null;
  const drivers = d ? DRIVERS.map((id) => d.kpis.find((k) => k.id === id)).filter((k) => !!k) : [];
  const survival = d?.kpis.find((k) => k.id === 'mrSurvival') ?? null;
  const limitAvg = limits.fresh ? avgPeak(limits.data) : null;
  const enough = !!d && d.summary.mrs >= MIN_MRS;
  const days = Math.max(
    1,
    Math.round((Date.parse(query.period.to) - Date.parse(query.period.from)) / 864e5) + 1,
  );
  const wastePct =
    d?.waste && d.waste.totalUsd > 0 ? Math.round((d.waste.wasteUsd / d.waste.totalUsd) * 100) : null;
  const levers = (d?.waste?.items ?? [])
    .filter((w) => w.usd > 0)
    .map((w) => ({ ...w, week: (w.usd / days) * 7 }))
    .sort((a, b) => b.week - a.week);

  return (
    <>
      <div className="hb-head">
        <Titles
          title="Эффективность"
          lead={d ? `${d.scopeLabel} · за ${nowLabel}: сколько стоит смёржённый MR и почему` : 'Считаем'}
        />
        <PeriodControls weeks={frame.weeks} change={frame.lastChange} state={picker} onChange={setPicker} />
      </div>
      <div className="ef-scales">
        <Scales />
      </div>
      <div className="dash hb-dash anx" data-busy={d && !data.fresh && !data.error ? '1' : undefined}>
        {data.error && !data.fresh ? (
          <Failure title="Не удалось загрузить эффективность" error={data.error} retry={data.reload} />
        ) : !d ? (
          <Skeleton />
        ) : (
          <div className="an-stage">
            <div className="mx ef">
              <section className="ef-main t-card" aria-label="Главный показатель">
                <div className="ef-tabs" role="tablist" aria-label="Главный показатель">
                  {(
                    [
                      ['price', 'Цена MR', enough ? formatKpi('usd', price?.value) : '—'],
                      ['mrs', 'Смёржено MR', String(d.summary.mrs)],
                      ['waste', 'Потери', wastePct === null ? '—' : `${wastePct}%`],
                      ['limit', 'Лимит недели', limitAvg === null ? '—' : `${Math.round(limitAvg)}%`],
                    ] as [Tab, string, string][]
                  ).map(([id, label, value]) => (
                    <button
                      key={id}
                      type="button"
                      role="tab"
                      className="ef-tab"
                      aria-selected={tab === id}
                      onClick={() => p.set({ t: id === 'price' ? null : id, k: null })}
                    >
                      <span className="ef-tab__t">{label}</span>
                      <b className="ef-tab__v">
                        <Count text={value} />
                      </b>
                      {id === 'price' && price && enough && (
                        <span className="ef-tab__d" data-tone={deltaOf(price).tone}>
                          {deltaOf(price).text} · {wasText(price)}
                        </span>
                      )}
                    </button>
                  ))}
                </div>
                {tab === 'price' &&
                  (enough && picked ? (
                    <Hero
                      kpi={picked}
                      chart={chart.data}
                      fresh={chart.fresh}
                      error={chart.error}
                      retry={chart.reload}
                      staff={staff}
                      mode={mode}
                      onMode={setMode}
                      shownKpi={shownKpi}
                      teamOrder={teamOrder}
                    />
                  ) : (
                    <p className="ef-note" role="status">
                      <b>Цены пока нет</b>
                      <span>
                        Покажем, когда наберётся {MIN_MRS} смёржённых MR: сейчас их {d.summary.mrs}.
                      </span>
                    </p>
                  ))}
                {tab === 'mrs' && (
                  <p className="ef-note">
                    <b>
                      {d.summary.mrs} {plural(d.summary.mrs, 'MR', 'MR', 'MR')} из {d.summary.sessions}{' '}
                      {plural(d.summary.sessions, 'сессии', 'сессий', 'сессий')}
                    </b>
                    <span>
                      {survival && survival.value !== null
                        ? `MR агента, дошедшие до main. Из тех, кому уже две недели, без отката осталось ${formatKpi(survival.format, survival.value)}.`
                        : 'MR агента, дошедшие до main. Долю MR, прожившую две недели без отката появится, когда наберётся достаточно MR старше двух недель.'}
                    </span>
                  </p>
                )}
                {tab === 'waste' && (
                  <p className="ef-note">
                    <b>
                      {d.waste
                        ? `${money(d.waste.wasteUsd)} из ${money(d.waste.totalUsd)}`
                        : 'Потери видит руководитель'}
                    </b>
                    <span>
                      {d.waste
                        ? 'Деньги, которые не дали результата. Корзины ниже: их можно складывать.'
                        : 'Откройте экран на уровне команды или организации.'}
                    </span>
                  </p>
                )}
                {tab === 'limit' &&
                  (limits.error && !limits.fresh ? (
                    <Failure title="Не удалось загрузить лимит" error={limits.error} retry={limits.reload} />
                  ) : !limits.fresh ? (
                    <p className="ef-note" role="status">
                      <b>Считаем</b>
                    </p>
                  ) : limits.data === null ? (
                    <p className="ef-note">
                      <b>Лимит видит руководитель</b>
                      <span>Откройте экран на уровне команды или организации.</span>
                    </p>
                  ) : (
                    <LimitHistoryView history={limits.data} />
                  ))}
              </section>

              <section className="ef-drivers" aria-label="Что двигает цену">
                <h3 className="anm-group__h">Что двигает цену</h3>
                <div className="anm-grid">
                  {drivers.map((k, i) => (
                    <Tile
                      key={k.id}
                      kpi={k}
                      index={i}
                      selected={driver === k.id}
                      nowLabel={nowLabel}
                      prevLabel={prevLabel}
                      onPick={(id) => p.set({ t: null, k: driver === id ? null : id })}
                      onHover={warm}
                    />
                  ))}
                </div>
              </section>

              {d.waste && staff && (
                <Card
                  title="Что включить"
                  lead="Оценка в неделю по потерям периода. Общей суммы нет: рычаги пересекаются"
                >
                  {levers.length === 0 ? (
                    <p className="an-empty">Заметных потерь нет, включать нечего</p>
                  ) : (
                    <ol className="ef-levers">
                      {levers.map((w) => (
                        <li key={w.id} className="ef-lever">
                          <span className="ef-lever__t">
                            <b>{w.title}</b>
                            <span>{w.text}</span>
                          </span>
                          <span className="ef-lever__v">≈ {money(w.week)} в неделю</span>
                        </li>
                      ))}
                    </ol>
                  )}
                </Card>
              )}
              <Changes changes={d.changes} />
              {staff && d.peers && <PeersCard peers={d.peers} teamOrder={teamOrder} />}
              <p className="ef-foot">
                Деньги по ценам API: на подписке это мера расхода, а не счёт. Результат — смёржённый MR
                агента. Сдвиг считается как неделя до против недели после; серым отмечены колебания в пределах
                обычных.
              </p>
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
      <div className="hb-head">
        <Titles title="Эффективность" lead="Считаем" />
      </div>
      <div className="dash hb-dash anx">
        {frame.error ? (
          <Failure title="Не удалось загрузить эффективность" error={frame.error} retry={frame.reload} />
        ) : (
          <Skeleton />
        )}
      </div>
    </>
  );
};

export default Screen;
