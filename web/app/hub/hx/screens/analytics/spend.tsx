// What the money screens add to the metric charts: the spend by day, stacked by team or model (budget line aside), and
// «Кто и на чём тратит» — the spend cut by team, person or model, a row opens its card.
import { lazy, Suspense, useMemo, useState } from 'react';

import type { AnalyticsQuery, SpendSplit } from '../../../org/api/analytics';
import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { navigate } from '../../../lib/router';
import { Btn } from '../../ui';
import { dataKey, useAnswer } from './store';
import { money } from './calc';

const Chart = lazy(() => import('./spend-chart'));

const Seg = <T extends string>({
  items,
  value,
  onChange,
  label,
}: {
  items: readonly (readonly [T, string])[];
  value: T;
  onChange: (v: T) => void;
  label: string;
}) => (
  <span className="mx-metric anx-mode" role="radiogroup" aria-label={label}>
    {items.map(([id, name]) => (
      <button
        key={id}
        type="button"
        role="radio"
        aria-checked={id === value}
        data-on={id === value ? 'true' : undefined}
        onClick={() => onChange(id)}
      >
        {name}
      </button>
    ))}
  </span>
);

const Failure = ({ error, retry }: { error: unknown; retry: () => void }) => (
  <p className="an-err" role="alert">
    <b>Не удалось загрузить</b>
    <span>{orgErrorText(error)}</span>
    <Btn size="sm" icon="rotate-cw" onClick={retry}>
      Повторить
    </Btn>
  </p>
);

/** «Сколько уходит на агентов»: the spend by day, stacked. */
export const SpendCard = ({ query, staff }: { query: AnalyticsQuery; staff: boolean }) => {
  const { api, shell } = useOrg();
  const [split, setSplit] = useState<'team' | 'model'>(staff ? 'team' : 'model');
  const key = `spend:${dataKey(query)}:${split}`;
  const a = useAnswer(
    key,
    () => api.spendDaily(query, split),
    () => undefined,
  );
  const names = useMemo(() => new Map(shell.teams.map((t) => [t.id, t.name])), [shell.teams]);
  const teamOrder = useMemo(() => shell.teams.map((t) => t.name), [shell.teams]);
  if (a.data === null && a.fresh) return null;
  return (
    <section className="dash-hero anx-hero" data-hx-part="chart">
      <div className="dash-hero__chart">
        <div className="anx-head">
          <div className="anx-k anx-what">
            <b>Сколько уходит на агентов</b>
            <span>Расход по дням в ценах API, по командам или моделям.</span>
          </div>
          {staff && (
            <Seg
              label="Разрез расхода"
              value={split}
              onChange={setSplit}
              items={[
                ['team', 'По командам'],
                ['model', 'По моделям'],
              ]}
            />
          )}
        </div>
        <div
          className="anx-viz"
          data-viz-mount="1"
          data-wait={a.data || a.error ? undefined : '1'}
          data-stale={a.data && !a.fresh && !a.error ? '1' : undefined}
        >
          {a.data && a.data.lines.length > 0 && (
            <Suspense fallback={null}>
              <Chart spend={a.data} names={names} teamOrder={teamOrder} />
            </Suspense>
          )}
        </div>
        {a.data && a.data.lines.length === 0 && <p className="md-note">За этот месяц расхода нет.</p>}
        {a.error != null && !a.fresh && <Failure error={a.error} retry={a.reload} />}
      </div>
    </section>
  );
};

/** «Кто и на чём тратит»: the rows are links to the team, the person or the models. */
export const WhereCard = ({ query, staff }: { query: AnalyticsQuery; staff: boolean }) => {
  const { api, shell, base } = useOrg();
  const [by, setBy] = useState<SpendSplit>(staff && shell.teams.length > 0 ? 'team' : 'model');
  const key = `where:${dataKey(query)}:${by}`;
  const a = useAnswer(
    key,
    () => api.spendBy(query, by),
    () => undefined,
  );
  const names = useMemo(() => new Map(shell.teams.map((t) => [t.id, t])), [shell.teams]);
  if (a.data === null && a.fresh) return null;
  const rows = [...(a.data ?? [])].sort((x, y) => y.usd - x.usd);
  const total = rows.reduce((s, r) => s + r.usd, 0) || 1;
  const top = rows[0]?.usd || 1;
  const go = (k: string) => {
    if (by === 'team') {
      const t = names.get(k);
      navigate(`${base}/team?t=${encodeURIComponent(t?.slug ?? k)}`);
    } else if (by === 'person') navigate(`${base}/teams?p=${encodeURIComponent(k)}`);
    else navigate(`${base}/accounts`);
  };
  const opts: readonly (readonly [SpendSplit, string])[] = staff
    ? [
        ['team', 'Команды'],
        ['person', 'Люди'],
        ['model', 'Модели'],
      ]
    : [['model', 'Модели']];
  return (
    <div className="t-card viz-card dash-tile hd-card an-where">
      <div className="ch">
        <h3 className="an-q">Кто и на чём тратит</h3>
        <div className="hd-r">
          {opts.length > 1 && <Seg label="Разрез" value={by} onChange={setBy} items={opts} />}
        </div>
      </div>
      {a.error != null && !a.fresh && <Failure error={a.error} retry={a.reload} />}
      <div className="an-rows" data-stale={a.data && !a.fresh ? '1' : undefined}>
        {rows.map((r) => (
          <button
            key={r.key || 'none'}
            type="button"
            className="an-row"
            onClick={() => go(r.key)}
            title={`${r.label || 'Без команды'}: ${money(r.usd)}, ${Math.round((r.usd / total) * 100)}% расхода.`}
          >
            <span className="an-row__n">
              <b>{(by === 'team' ? names.get(r.key)?.name : null) ?? (r.label || 'Без команды')}</b>
              <span className="mut2 sm">{r.calls.toLocaleString('ru-RU')} вызовов</span>
            </span>
            <span className="an-row__b">
              <i style={{ width: `${((r.usd / top) * 100).toFixed(1)}%` }} />
            </span>
            <span className="num an-row__v">
              {money(r.usd)}
              <span className="mut2 sm">{Math.round((r.usd / total) * 100)}%</span>
            </span>
          </button>
        ))}
        {a.data && rows.length === 0 && <p className="md-note">Расхода за период нет.</p>}
      </div>
    </div>
  );
};

/** «Подписки загружены»: how near the weekly windows of the subscriptions came to their limit. */
export const SubsCard = ({ query }: { query: AnalyticsQuery }) => {
  const { api } = useOrg();
  const a = useAnswer(
    `subs:${dataKey(query)}`,
    () => api.limitWindows(query),
    () => undefined,
  );
  const rows = (a.data ?? [])
    .map((acc) => ({ acc, w: acc.windows.find((x) => x.name === 'weekly') }))
    .filter(
      (r): r is { acc: (typeof r)['acc']; w: NonNullable<typeof r.w> } =>
        r.w !== undefined && r.w.cycles.length > 0,
    );
  if (rows.length === 0) return null;
  const avg = rows.reduce((s, r) => s + r.w.avgPeakPct, 0) / rows.length;
  const tone = (p: number) => (p > 85 ? 'bad' : p > 65 ? 'warn' : 'ok');
  return (
    <div className="t-card viz-card dash-tile hd-card an-where">
      <div className="ch">
        <h3 className="an-q">Подписки загружены на {Math.round(avg)}%</h3>
      </div>
      <p className="md-note">Средний пик недельного окна подписок: какую долю лимита выбирают за неделю.</p>
      <div className="an-rows">
        {rows.map(({ acc, w }) => (
          <div key={acc.id} className="an-row">
            <span className="an-row__n">
              <b>{acc.label}</b>
              <span className="mut2 sm">
                {acc.plan}
                {w.fullCycles > 0 ? ` · упёрлась в лимит: ${w.fullCycles} из ${w.cycles.length} нед.` : ''}
              </span>
            </span>
            <span
              className="hd-meter"
              data-t={tone(w.avgPeakPct)}
              style={{ ['--w' as string]: `${Math.min(100, Math.max(2, w.avgPeakPct))}%` }}
            >
              <i />
            </span>
            <span className="num an-row__v">{Math.round(w.avgPeakPct)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
};
