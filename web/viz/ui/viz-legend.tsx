// SPDX-License-Identifier: Apache-2.0
import { useRef } from 'react';

import { classNames } from '../internal/cx';
import { VizSwatch } from './primitives';

import '../viz.css';

export interface VizLegendItem<Id extends string = string> {
  readonly id: Id;
  readonly label: string;
  readonly color: string;
}

export interface VizLegendProps<Id extends string = string> {
  readonly items: readonly VizLegendItem<Id>[];
  readonly hidden?: ReadonlySet<Id>;
  /** Серия под курсором: остальные пункты приглушаются. */
  readonly focus?: Id | null;
  /** Клик — скрыть/показать. */
  readonly onToggle?: (id: Id) => void;
  /** Двойной клик — оставить только эту серию. */
  readonly onSolo?: (id: Id) => void;
  /** Наведение и фокус клавиатурой — подсветить серию на графике. */
  readonly onFocus?: (id: Id | null) => void;
  /** Скелетон вместо пунктов, пока серии неизвестны. */
  readonly loading?: boolean;
  /** Одна серия легенды не требует — заголовок её уже называет. `always` показывает и одну. */
  readonly always?: boolean;
  /** `line` — ключ чертой (линейные графики), `dot` — точкой. */
  readonly swatch?: 'dot' | 'line';
  readonly className?: string;
  readonly 'aria-label'?: string;
}

const SKELETON_WIDTHS = ['s', 'l', 'xs', 'm', 's'] as const;
/** Пауза между первым кликом и решением «это одиночный»: иначе двойной клик сначала скрыл бы серию. */
const DOUBLE_CLICK_WAIT = 200;

/**
 * Легенда серий: цветовой ключ и название, без чисел. Общая для всех графиков блока —
 * волн, столбиков, долей. Состояние держит `useVizSeriesState`.
 */
export function VizLegend<Id extends string>({
  items,
  hidden,
  focus = null,
  onToggle,
  onSolo,
  onFocus,
  loading = false,
  always = false,
  swatch = 'dot',
  className,
  'aria-label': ariaLabel = 'Легенда',
}: VizLegendProps<Id>) {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  if (loading) {
    return (
      <div className={classNames('viz-legend', 'viz-legend--loading', className)} aria-hidden="true">
        {SKELETON_WIDTHS.map((w, i) => (
          <span key={i} className={`viz-legend__skeleton viz-legend__skeleton--${w}`} />
        ))}
      </div>
    );
  }
  if (items.length < 2 && !always) return null;
  const interactive = !!onToggle || !!onSolo;
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={classNames('viz-legend', focus !== null && 'viz-legend--focused', className)}
    >
      {items.map((item) => {
        const off = hidden?.has(item.id) ?? false;
        const content = (
          <>
            <VizSwatch color={item.color} shape={swatch} off={off} />
            <span className="viz-legend__label">{item.label}</span>
          </>
        );
        if (!interactive) {
          return (
            <span
              key={item.id}
              className="viz-legend__item"
              data-off={off || undefined}
              data-focus={focus === item.id || undefined}
            >
              {content}
            </span>
          );
        }
        return (
          <button
            key={item.id}
            type="button"
            className="viz-legend__item"
            aria-pressed={!off}
            data-off={off || undefined}
            data-focus={focus === item.id || undefined}
            onPointerEnter={() => !off && onFocus?.(item.id)}
            onPointerLeave={() => onFocus?.(null)}
            onFocus={() => !off && onFocus?.(item.id)}
            onBlur={() => onFocus?.(null)}
            onClick={(event) => {
              if (timer.current) clearTimeout(timer.current);
              if (event.detail > 1) return;
              // Клавиатура (detail 0) и легенда без «только эта» — сразу.
              const wait = event.detail === 0 || !onSolo ? 0 : DOUBLE_CLICK_WAIT;
              timer.current = setTimeout(() => onToggle?.(item.id), wait);
            }}
            onDoubleClick={() => {
              if (timer.current) clearTimeout(timer.current);
              onSolo?.(item.id);
            }}
          >
            {content}
          </button>
        );
      })}
    </div>
  );
}
