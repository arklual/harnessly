// Small pictures of the machines screen: the group's state strip, a load bar of a row, a day's load curve. All SVG,
// so sizes are attributes and not inline styles.
import { strip } from './format';

export const StateStrip = ({ online, idle, offline }: { online: number; idle: number; offline: number }) => {
  const parts = strip({ online, idle, offline });
  return (
    <svg className="hm-strip" width="100%" height="8" aria-hidden="true">
      {parts.map((p, i) => {
        if (p.pct <= 0) return null;
        const at = parts.slice(0, i).reduce((n, q) => n + q.pct, 0);
        return (
          <rect
            key={p.tone}
            className={`hm-strip__${p.tone}`}
            x={`${at}%`}
            width={`${Math.max(p.pct - 1, 0.5)}%`}
            height="8"
            rx="4"
          />
        );
      })}
    </svg>
  );
};

/** The thin bar of a row's CPU, memory or disk: grey while calm, amber from 70, red from 85. */
export const LoadBar = ({ pct }: { pct: number | null }) =>
  pct === null ? (
    <span className="hm-load">
      <svg width="100%" height="4" aria-hidden="true">
        <rect className="hm-load__track" width="100%" height="4" rx="2" />
      </svg>
      <span className="hm-load__v">—</span>
    </span>
  ) : (
    <span className="hm-load">
      <svg width="100%" height="4" aria-hidden="true">
        <rect className="hm-load__track" width="100%" height="4" rx="2" />
        <rect
          className={pct >= 85 ? 'hm-load__bad' : pct >= 70 ? 'hm-load__warn' : 'hm-load__fill'}
          width={`${pct}%`}
          height="4"
          rx="2"
        />
      </svg>
      <span className="hm-load__v">{pct}%</span>
    </span>
  );

export const Spark = ({ values, hot }: { values: readonly number[]; hot: boolean }) => {
  const max = 100;
  const step = 100 / Math.max(1, values.length - 1);
  const pts = values
    .map((v, i) => `${(i * step).toFixed(2)},${(100 - (v / max) * 100).toFixed(2)}`)
    .join(' ');
  return (
    <svg className="hm-spark" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
      <polyline
        className={hot ? 'hm-spark__hot' : 'hm-spark__line'}
        points={pts}
        fill="none"
        vectorEffect="non-scaling-stroke"
      />
    </svg>
  );
};
