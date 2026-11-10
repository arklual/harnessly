// The big chart: what the selected metric is, how to show it, and the chart itself. The chart's module is loaded on
// its own; while it (or the series of a new metric) is on its way the old picture stays, dimmed, or the place is held.
import { lazy, Suspense } from 'react';

import type { Kpi, KpiChart } from '../../../org/api/analytics';
import { OrgApiError, orgErrorText } from '../../../org/api/errors';
import { Btn } from '../../ui';
import type { ChartMode } from './chart';

const Chart = lazy(() => import('./chart'));

const MODES: readonly (readonly [ChartMode, string])[] = [
  ['teams', 'По командам'],
  ['cmp', 'Против периода сравнения'],
];

export interface HeroProps {
  /** The metric chosen now: its name and hint show at once. */
  kpi: Kpi;
  /** The last series that came, for any metric. */
  chart: KpiChart | null;
  /** The chart is the one of `kpi` and of the period asked now. */
  fresh: boolean;
  error: unknown;
  retry: () => void;
  /** Only a lead or an admin can switch; a participant has the comparison alone. */
  staff: boolean;
  mode: ChartMode;
  onMode: (m: ChartMode) => void;
  /** The kpi the shown chart belongs to (it can be the previous one while the next is on its way). */
  shownKpi: Kpi | null;
  teamOrder: readonly string[];
}

export const Hero = ({
  kpi,
  chart,
  fresh,
  error,
  retry,
  staff,
  mode,
  onMode,
  shownKpi,
  teamOrder,
}: HeroProps) => {
  // A person of no team role has no daily series yet (HRN-970): that is a note, not a failure.
  const personal = error instanceof OrgApiError && error.code === 'not_ready';
  const failed = error != null && !fresh && !personal;
  const picture = chart !== null && shownKpi !== null;
  return (
    <section className="dash-hero anx-hero">
      <div className="dash-hero__chart">
        <div className="anx-head">
          <div className="anx-k anx-what">
            <b>{kpi.title}</b>
            <span>{kpi.hint}</span>
          </div>
          {staff && (
            <span className="mx-metric anx-mode" role="radiogroup" aria-label="Как показать">
              {MODES.map(([id, label]) => (
                <button
                  key={id}
                  type="button"
                  role="radio"
                  aria-checked={id === mode}
                  data-on={id === mode ? 'true' : undefined}
                  onClick={() => onMode(id)}
                >
                  {label}
                </button>
              ))}
            </span>
          )}
        </div>
        <div
          className="anx-viz"
          data-viz-mount="1"
          data-wait={picture || failed || personal ? undefined : '1'}
          data-stale={picture && !fresh && !failed ? '1' : undefined}
        >
          {picture && (
            <Suspense fallback={null}>
              <Chart chart={chart} kpi={shownKpi} mode={mode} teamOrder={teamOrder} />
            </Suspense>
          )}
        </div>
        {personal && (
          <p className="md-note">
            График по дням считается для команды и тенанта, по одному человеку его пока нет.
          </p>
        )}
        {failed && (
          <p className="an-err" role="alert">
            <b>Не удалось загрузить график</b>
            <span>{orgErrorText(error)}</span>
            <Btn size="sm" icon="rotate-cw" onClick={retry}>
              Повторить
            </Btn>
          </p>
        )}
      </div>
    </section>
  );
};
