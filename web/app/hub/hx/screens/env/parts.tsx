// The pieces the screens share: the summary tiles, the loading skeleton, the failure row.
import type { CSSProperties, ReactNode } from 'react';

import type { EnvResponse } from '../../../../hub-api/env';
import { OrgApiError, orgErrorText } from '../../../org/api/errors';
import { Btn, Icon } from '../../ui';
import { type Part, type Tile, tilesOf } from './helpers';

type Vars = CSSProperties & Record<`--${string}`, string | number>;

/** A strip of parts side by side (`.sx-strip`); a part with no weight is not drawn. */
const SegmentStrip = ({ parts }: { parts: Part[] }) => (
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
const MiniBars = ({ values }: { values: number[] }) => {
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

const StatTile = ({ index, tile }: { index: number; tile: Tile }) => (
  <div className="sx-tile mo-tile" style={{ '--sx-i': index, '--sx-tone': tile.tone } as Vars}>
    <span className="sx-tile__top">
      <span className="sx-tile__icon">
        <Icon i={tile.icon} size="sm" />
      </span>
    </span>
    <span className="sx-tile__chart">
      {'strip' in tile.chart ? (
        <SegmentStrip parts={tile.chart.strip} />
      ) : (
        <MiniBars values={tile.chart.bars} />
      )}
    </span>
    <span className="sx-tile__label">{tile.label}</span>
    <span className="sx-tile__value">{tile.value}</span>
  </div>
);

export const Tiles = ({ view }: { view: EnvResponse }) => (
  <div className="dash ad-dash mo-tiles">
    <div className="sx-wrap">
      <div className="sx-grid">
        {tilesOf(view).map((t, i) => (
          <StatTile key={t.label} index={i} tile={t} />
        ))}
      </div>
    </div>
  </div>
);

/** The shimmering boxes of the screen while the hub answers. */
export const EnvSkeleton = ({ tiles = true, rows = 5 }: { tiles?: boolean; rows?: number }) => (
  <div className="env-sk" aria-busy="true" aria-label="Загружаем">
    {tiles && (
      <div className="env-sk__tiles">
        {[0, 1, 2, 3].map((i) => (
          <span key={i} className="hx-sk" style={{ height: 118 }} />
        ))}
      </div>
    )}
    <span className="hx-sk" style={{ height: 44, width: '60%' }} />
    <div className="env-sk__rows">
      {Array.from({ length: rows }, (_, i) => (
        <span key={i} className="hx-sk" style={{ height: 56 }} />
      ))}
    </div>
  </div>
);

/** One row: what went wrong and the way to try again. */
export const LoadFailed = ({ error, onRetry }: { error: unknown; onRetry: () => void }) => (
  <div className="env-fail" role="alert">
    <Icon i="circle-alert" />
    <span>{orgErrorText(error)}</span>
    <Btn size="sm" onClick={onRetry}>
      Повторить
    </Btn>
  </div>
);

export const Empty = ({ children }: { children: ReactNode }) => <div className="mo-empty">{children}</div>;

/** The feature is off for the organization: the hub answers `not_enabled`, the screen says there is nothing. */
export const isNotEnabled = (e: unknown): boolean => e instanceof OrgApiError && e.code === 'not_enabled';
