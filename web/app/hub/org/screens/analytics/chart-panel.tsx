import { lazy, Suspense, useRef, useState } from 'react';
import { Minus, Plus } from 'lucide-react';

import { IconButton, useTheme } from '@kit';

import type { Kpi, KpiChart } from '../../api/analytics';
import { LoadError, Loading } from '../../ui/page';
import type { ChartMode, ZoomHandle } from './chart';
import { lineColors } from './chart-colors';

// The chart and ECharts under it are a chunk of their own.
const KpiChartView = lazy(() => import('./chart'));

const MODES: { id: ChartMode; label: string }[] = [
  { id: 'teams', label: 'По командам' },
  { id: 'compare', label: 'Против периода сравнения' },
];

export const ChartPanel = ({
  kpi,
  chart,
  error,
  retry,
  lineNoun,
  periodName,
  compareName,
}: {
  kpi: Kpi;
  chart: KpiChart | null;
  error: unknown;
  retry: () => void;
  /** «командам» for an admin, «людям» for a lead: the first tab's name. */
  lineNoun: string;
  periodName: string;
  compareName: string;
}) => {
  const [mode, setMode] = useState<ChartMode>('teams');
  const { resolved } = useTheme();
  const dark = resolved === 'dark';
  const zoom = useRef<ZoomHandle | null>(null);
  const colors = lineColors(dark);
  const stacked = kpi.format === 'usd';
  const solo = chart?.lines.length === 1;
  return (
    <section className="an-card an-chart" aria-labelledby="an-chart-title">
      <header className="an-chart__head">
        <div>
          <h2 id="an-chart-title" className="an-card__title">
            {kpi.title}
          </h2>
          <p className="an-card__sub">{kpi.hint}</p>
        </div>
        <div className="an-tabs" role="tablist" aria-label="Вид графика">
          {MODES.map((m) => (
            <button
              key={m.id}
              type="button"
              role="tab"
              aria-selected={mode === m.id}
              className="an-tab"
              onClick={() => setMode(m.id)}
            >
              {m.id === 'teams' ? `По ${lineNoun}` : m.label}
            </button>
          ))}
        </div>
      </header>
      <div className="an-chart__bar">
        {mode === 'teams' && chart && !solo ? (
          <ul className="an-legend">
            {chart.lines.map((l, i) => (
              <li key={l.id}>
                <i className="an-legend__dot" data-c={i % colors.length} />
                {l.name}
              </li>
            ))}
          </ul>
        ) : mode === 'compare' ? (
          <ul className="an-legend">
            <li>
              <i className="an-legend__dot" data-c="0" />
              {periodName}
            </li>
            <li>
              <i className="an-legend__dot" data-c="dash" />
              {compareName}
            </li>
          </ul>
        ) : (
          <span />
        )}
        {mode === 'teams' && (
          <div className="an-zoom">
            <IconButton label="Отдалить" onClick={() => zoom.current?.by(1.6)}>
              <Minus size={15} />
            </IconButton>
            <IconButton label="Приблизить" onClick={() => zoom.current?.by(0.625)}>
              <Plus size={15} />
            </IconButton>
          </div>
        )}
      </div>
      <div className="an-chart__body">
        {error ? (
          <LoadError error={error} retry={retry} />
        ) : !chart ? (
          <Loading />
        ) : (
          <Suspense fallback={<Loading />}>
            <KpiChartView
              chart={chart}
              format={kpi.format}
              stacked={stacked}
              mode={mode}
              dark={dark}
              zoomRef={zoom}
              ariaLabel={`${kpi.title}: график`}
              periodName={periodName}
              compareName={compareName}
            />
          </Suspense>
        )}
      </div>
    </section>
  );
};
