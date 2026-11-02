import { inRubles } from '../../money';
import { monthWord } from '../../dates';
import { useState } from 'react';

import { Link } from '../../../lib/router';
import type { Money, OverviewData, TeamSpend as TeamSpendRow } from '../../api/overview';
import { Seg } from '../../ui/page';
import { num, rub, tokens, usd } from './format';
import { FillRow, Gauge } from './svg';

type Unit = 'rub' | 'usd' | 'tokens';
/** The first unit is the shown currency; while the hub knows no rate it is the dollar itself, offered once. */
const unitsNow = () =>
  inRubles()
    ? ([
        { value: 'rub', label: '₽' },
        { value: 'usd', label: '$' },
        { value: 'tokens', label: 'Токены' },
      ] as const)
    : ([
        { value: 'usd', label: '$' },
        { value: 'tokens', label: 'Токены' },
      ] as const);

const show = (m: Money, unit: Unit): string =>
  unit === 'rub' ? rub(m.rub) : unit === 'usd' ? usd(m.usd) : `${tokens(m.tokens)}`;

/** «Расход по командам» and «Эффективность»: the ring by team, and how many tasks a person closes. */
export const TeamSpend = ({ data, base }: { data: NonNullable<OverviewData['byTeam']>; base: string }) => {
  const [unit, setUnit] = useState<Unit>(inRubles() ? 'rub' : 'usd');
  const total = data.total[unit] || 1;
  const rows: TeamSpendRow[] = data.teams;
  const best = Math.max(...data.efficiency.map((e) => e.tasksPerPerson), 1);
  return (
    <div className="ov-pair">
      <section className="ov-card" aria-labelledby="ov-spend">
        <div className="ov-card__head">
          <h2 id="ov-spend" className="ov-card__title">
            Расход по командам
          </h2>
          <Seg label="Единицы" value={unit} options={unitsNow()} onChange={setUnit} />
        </div>
        <Gauge parts={rows.map((t, i) => ({ id: t.id, value: t[unit], tone: i }))}>
          <b>{show(data.total, unit)}</b>
          <small>за {monthWord()}</small>
        </Gauge>
        <ul className="ov-legend">
          {rows.map((t, i) => (
            <li key={t.id}>
              <span className="ov-legend__dot" data-tone={i} aria-hidden="true" />
              <span className="ov-legend__name">{t.name}</span>
              <span className="ov-legend__pct">{num((t[unit] / total) * 100, 0)}%</span>
              <b className="ov-legend__val">{show(t, unit)}</b>
            </li>
          ))}
        </ul>
      </section>
      {data.efficiency.length > 0 && (
        <section className="ov-card" aria-labelledby="ov-eff">
          <div className="ov-card__head">
            <h2 id="ov-eff" className="ov-card__title">
              Эффективность
            </h2>
            <Link to={`${base}/analytics`} className="ov-card__go" aria-label="Открыть аналитику">
              ↗
            </Link>
          </div>
          <p className="ov-card__sub">Закрытые задачи на человека за месяц</p>
          <div className="ov-rows">
            {data.efficiency.map((e, i) => (
              <FillRow key={e.id} pct={(e.tasksPerPerson / best) * 100} tone="plain">
                <span className="ov-row__text">
                  <b>{e.name}</b>
                  <small>{e.resultPct}% чатов дошли до результата</small>
                </span>
                <span className="ov-row__val">
                  {num(e.tasksPerPerson, 1, true)}
                  {i === 0 ? ' задачи на чел.' : ''}
                </span>
              </FillRow>
            ))}
          </div>
        </section>
      )}
    </div>
  );
};
