// The twelve tiles in three groups: the value, how it moved, where it was, and the thirty-day line of the period over
// the dashed line of the comparison. A tile picks the metric of the big chart; its bubble says what the number is.
import { type CSSProperties, memo, useMemo } from 'react';

import type { Kpi, KpiGroup, KpiId } from '../../../org/api/analytics';
import { deltaOf, formatKpi, sparkPaths, SPARK_H, SPARK_W, wasText } from './calc';

const GROUPS: { id: KpiGroup; title: string }[] = [
  { id: 'result', title: 'Результат' },
  { id: 'agent', title: 'Как работает агент' },
  { id: 'env', title: 'Окружение' },
];

const NOW_HEX = '#8968f4';

/** Why a tile has no number: the MR ones wait for merged MRs, the rest for five sessions. */
const emptyText = (id: KpiId): string =>
  id === 'moneyPerMr' || id === 'mrPerPerson' || id === 'timeToMr'
    ? 'нет смёрженных MR за период'
    : 'мало сессий — нужно от пяти';

interface TileProps {
  kpi: Kpi;
  index: number;
  selected: boolean;
  nowLabel: string;
  prevLabel: string;
  onPick: (id: KpiId) => void;
  onHover: (id: KpiId) => void;
}

const Tile = memo(({ kpi, index, selected, nowLabel, prevLabel, onPick, onHover }: TileProps) => {
  const delta = useMemo(() => deltaOf(kpi), [kpi]);
  const paths = useMemo(() => sparkPaths(kpi.series, kpi.prevSeries), [kpi.series, kpi.prevSeries]);
  const none = kpi.value === null;
  return (
    <button
      type="button"
      className="anm anx-hov"
      style={{ '--i': index } as CSSProperties}
      aria-pressed={selected}
      data-none={none ? '1' : undefined}
      onClick={() => onPick(kpi.id)}
      onPointerEnter={() => onHover(kpi.id)}
      onFocus={() => onHover(kpi.id)}
    >
      <span className="anm__t">{kpi.title}</span>
      <span className="anm__row">
        <span className="anm__v" data-none={none ? '1' : undefined}>
          {formatKpi(kpi.format, kpi.value)}
        </span>
        <span className="anm__d" data-tone={delta.tone}>
          {delta.text}
        </span>
      </span>
      <svg
        className="anm-spark"
        viewBox={`0 0 ${SPARK_W} ${SPARK_H}`}
        preserveAspectRatio="none"
        aria-hidden="true"
        data-tone={delta.tone}
      >
        <path d={paths.prev} className="anm-prev" />
        <path d={paths.area} className="anm-area" />
        <path d={paths.now} className="anm-now" />
      </svg>
      <span className="anm__was">{none ? emptyText(kpi.id) : wasText(kpi)}</span>
      <span className="anx-pop">
        <span className="viz-tip">
          <span className="viz-tip__title">{kpi.title}</span>
          <span className="anm-pop__what">{kpi.hint}</span>
          <span className="viz-tip__row">
            <svg className="viz-swatch" viewBox="0 0 10 10">
              <circle cx="5" cy="5" r="4" fill={NOW_HEX} />
            </svg>
            <span>{nowLabel}</span>
            <b>{formatKpi(kpi.format, kpi.value)}</b>
          </span>
          <span className="viz-tip__row">
            <svg className="viz-swatch" viewBox="0 0 14 10">
              <line x1="0" x2="14" y1="5" y2="5" stroke="#a1a1aa" strokeWidth="2" strokeDasharray="4 3" />
            </svg>
            <span>{prevLabel}</span>
            <b>{formatKpi(kpi.format, kpi.prev)}</b>
          </span>
          <span className="viz-tip__note">Нажми — откроется на большом графике</span>
        </span>
      </span>
    </button>
  );
});
Tile.displayName = 'Tile';

export interface TilesProps {
  kpis: readonly Kpi[];
  selected: KpiId;
  nowLabel: string;
  prevLabel: string;
  onPick: (id: KpiId) => void;
  onHover: (id: KpiId) => void;
}

export const Tiles = ({ kpis, selected, nowLabel, prevLabel, onPick, onHover }: TilesProps) => (
  <div className="anm-wrap">
    {GROUPS.map((g) => {
      const items = kpis.filter((k) => k.group === g.id);
      if (items.length === 0) return null;
      return (
        <div key={g.id} className="anm-group">
          <div className="anm-group__h">{g.title}</div>
          <div className="anm-grid">
            {items.map((k) => (
              <Tile
                key={k.id}
                kpi={k}
                index={kpis.indexOf(k)}
                selected={k.id === selected}
                nowLabel={nowLabel}
                prevLabel={prevLabel}
                onPick={onPick}
                onHover={onHover}
              />
            ))}
          </div>
        </div>
      );
    })}
  </div>
);
