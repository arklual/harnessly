// The soft tile of the board's Обзор / Команды / Машины heads: an icon, a picture (bars or a progress line), a bold
// title and one grey line. Not a counter-badge: the title is the point.
import type { ReactNode } from 'react';
import { ArrowUpRight } from 'lucide-react';

import { Link } from '../../lib/router';

export type TileTone = 'violet' | 'teal' | 'green' | 'amber' | 'gray' | 'rose' | 'blue';

export interface TileProps {
  tone: TileTone;
  icon: ReactNode;
  title: string;
  line: ReactNode;
  /** The picture: `<MiniBars/>`, `<ProgressLine/>`. */
  viz?: ReactNode;
  /** Where the tile leads; absent: it is not a link. */
  to?: string;
}

export const Tile = ({ tone, icon, title, line, viz, to }: TileProps) => {
  const inner = (
    <>
      <span className="ho-tile__top">
        <span className="ho-tile__icon">{icon}</span>
        {to && <ArrowUpRight size={14} className="ho-tile__go" aria-hidden="true" />}
      </span>
      {viz != null && <span className="ho-tile__viz">{viz}</span>}
      <span className="ho-tile__title">{title}</span>
      <span className="ho-tile__line">{line}</span>
    </>
  );
  return to ? (
    <Link to={to} className="ho-tile" data-tone={tone}>
      {inner}
    </Link>
  ) : (
    <div className="ho-tile" data-tone={tone}>
      {inner}
    </div>
  );
};

/**
 * A row of bars, the last one stronger: a short history. Values are relative; the bars scale to the largest.
 * Drawn in SVG so the sizes are attributes, not inline styles.
 */
export const MiniBars = ({ values }: { values: readonly number[] }) => {
  const max = Math.max(1, ...values);
  const n = Math.max(1, values.length);
  const step = 100 / n;
  return (
    <svg className="ho-bars" width="100%" height="28" aria-hidden="true">
      {values.map((v, i) => {
        const h = Math.max(8, Math.round((v / max) * 100));
        return (
          <rect
            key={i}
            className={i === n - 1 ? 'ho-bars__last' : 'ho-bars__bar'}
            x={`${i * step + step * 0.12}%`}
            width={`${step * 0.76}%`}
            y={`${100 - h}%`}
            height={`${h}%`}
            rx="2"
          />
        );
      })}
    </svg>
  );
};

/** A thin progress line of a share 0..1. */
export const ProgressLine = ({ value }: { value: number }) => (
  <svg className="ho-prog" width="100%" height="6" aria-hidden="true">
    <rect className="ho-prog__track" width="100%" height="6" rx="3" />
    <rect
      className="ho-prog__fill"
      width={`${Math.round(Math.min(1, Math.max(0, value)) * 100)}%`}
      height="6"
      rx="3"
    />
  </svg>
);
