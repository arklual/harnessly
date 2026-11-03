// The stat tile of the board's pages: an icon, a small chart (a strip of parts or bars), a label and a value line.
import type { CSSProperties, ReactNode } from 'react';

import { Icon } from '../ui';
import './stat-tile.css';

type Vars = CSSProperties & Record<`--${string}`, string | number>;

export interface StripPart {
  /** The width of the part relative to the others. */
  weight: number;
  /** Absent: the muted rest of the strip. */
  color?: string;
}

/** A strip of parts side by side (`.sx-strip`); a part with no weight is not drawn. */
export const SegmentStrip = ({ parts }: { parts: StripPart[] }) => (
  <span className="sx-strip" aria-hidden="true">
    {parts
      .filter((p) => p.weight > 0)
      .map((p, i) => (
        <span
          key={i}
          className={p.color ? 'sx-strip__part' : 'sx-strip__part sx-strip__part--muted'}
          style={{ '--sx-w': p.weight, ...(p.color ? { '--sx-c': p.color } : {}) } as Vars}
        />
      ))}
  </span>
);

/** Bars on a 100 x 40 canvas, the last one in the full tone (`.sx-bars`). */
export const MiniBars = ({ values }: { values: number[] }) => {
  const n = Math.max(1, values.length);
  const max = Math.max(1, ...values);
  const step = 98 / n;
  const w = step * 0.72;
  return (
    <svg className="sx-bars" viewBox="0 0 100 40" preserveAspectRatio="none" aria-hidden="true">
      {values.map((v, i) => {
        const h = Math.max(2, (v / max) * 38);
        return (
          <rect
            key={i}
            x={(1 + i * step).toFixed(2)}
            y={(40 - h).toFixed(2)}
            width={w.toFixed(2)}
            height={h.toFixed(2)}
            rx="1.2"
            data-recent={i === n - 1 ? 'true' : undefined}
          />
        );
      })}
    </svg>
  );
};

export const StatTile = ({
  index,
  tone,
  icon,
  chart,
  label,
  value,
}: {
  index: number;
  tone: string;
  icon: string;
  chart: ReactNode;
  label: string;
  value: string;
}) => (
  <div className="sx-tile mo-tile" style={{ '--sx-i': index, '--sx-tone': tone } as Vars}>
    <span className="sx-tile__top">
      <span className="sx-tile__icon">
        <Icon i={icon} size="sm" />
      </span>
    </span>
    <span className="sx-tile__chart">{chart}</span>
    <span className="sx-tile__label">{label}</span>
    <span className="sx-tile__value">{value}</span>
  </div>
);

/** The row of tiles with the board's wrappers. */
export const StatTiles = ({ children }: { children: ReactNode }) => (
  <div className="dash ad-dash mo-tiles">
    <div className="sx-wrap">
      <div className="sx-grid">{children}</div>
    </div>
  </div>
);
