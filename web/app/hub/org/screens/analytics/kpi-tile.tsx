import type { Kpi } from '../../api/analytics';
import { deltaOf, formatValue, wasText } from './format';

const W = 100;
const H = 32;

const points = (v: readonly number[], lo: number, hi: number): string =>
  v
    .map((y, i) => {
      const x = v.length > 1 ? (i / (v.length - 1)) * W : 0;
      const py = H - 2 - ((y - lo) / (hi - lo || 1)) * (H - 4);
      return `${x.toFixed(2)},${py.toFixed(2)}`;
    })
    .join(' ');

/** The line of this period over the dotted line of the compare period; both on one scale. */
const Spark = ({ kpi, tone }: { kpi: Kpi; tone: string }) => {
  const now = kpi.series.filter((v): v is number => v !== null);
  const prev = kpi.prevSeries.filter((v): v is number => v !== null);
  const all = [...now, ...prev];
  const lo = Math.min(...all);
  const hi = Math.max(...all);
  const line = points(now, lo, hi);
  return (
    <svg
      className="an-spark"
      data-tone={tone}
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      width="100%"
      height="44"
      aria-hidden="true"
    >
      <polygon className="an-spark__area" points={`0,${H} ${line} ${W},${H}`} />
      <polyline className="an-spark__prev" points={points(prev, lo, hi)} />
      <polyline className="an-spark__line" points={line} />
    </svg>
  );
};

export const KpiTile = ({
  kpi,
  selected,
  onSelect,
}: {
  kpi: Kpi;
  selected: boolean;
  onSelect: () => void;
}) => {
  const d = deltaOf(kpi);
  return (
    <button type="button" className="an-kpi" aria-pressed={selected} onClick={onSelect}>
      <span className="an-kpi__title">{kpi.title}</span>
      <span className="an-kpi__row">
        <b className="an-kpi__value">{formatValue(kpi.format, kpi.value)}</b>
        {d.tone === 'flat' ? (
          <span className="an-kpi__same">{kpi.value === null ? 'мало сессий' : 'как раньше'}</span>
        ) : (
          <span className="an-kpi__delta" data-tone={d.tone}>
            {d.text}
          </span>
        )}
      </span>
      {kpi.series.length > 0 && <Spark kpi={kpi} tone={d.tone === 'flat' ? 'flat' : d.tone} />}
      <span className="an-kpi__was">{wasText(kpi)}</span>
    </button>
  );
};
