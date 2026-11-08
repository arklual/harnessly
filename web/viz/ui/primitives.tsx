// SPDX-License-Identifier: Apache-2.0
import { forwardRef, type HTMLAttributes, useCallback, useLayoutEffect, useRef } from 'react';

import { classNames } from '../internal/cx';

import '../viz.css';

export interface VizSwatchProps {
  /** Любой CSS-цвет серии. */
  readonly color: string;
  /** `line` — короткая черта (легенда линий), `dot` — точка (подсказки, сводки). */
  readonly shape?: 'dot' | 'line';
  /** Скрытая серия: пустой контур вместо заливки. */
  readonly off?: boolean;
  readonly className?: string;
}

/**
 * Цветовой ключ серии. Цвет — атрибут SVG, а не инлайн-стиль: серий может быть сколько угодно,
 * а класс на каждый цвет не заведёшь.
 */
export function VizSwatch({ color, shape = 'dot', off = false, className }: VizSwatchProps) {
  if (shape === 'line') {
    return (
      <svg
        className={classNames('viz-swatch', 'viz-swatch--line', className)}
        viewBox="0 0 14 6"
        aria-hidden="true"
      >
        <rect
          x="1"
          y="1.5"
          width="12"
          height="3"
          rx="1.5"
          fill={off ? 'transparent' : color}
          stroke={color}
          strokeWidth={off ? 1 : 0}
        />
      </svg>
    );
  }
  return (
    <svg className={classNames('viz-swatch', className)} viewBox="0 0 10 10" aria-hidden="true">
      <circle cx="5" cy="5" r="4" fill={off ? 'transparent' : color} stroke={color} strokeWidth="1.5" />
    </svg>
  );
}

export interface VizAnchorPoint {
  readonly x: number;
  readonly y: number;
  /** Ширина с центром в `x`: пузырь встаёт сбоку от неё (столбик), а не поверх. */
  readonly width?: number;
}

export interface VizAnchorProps extends HTMLAttributes<HTMLSpanElement> {
  /** Точка в координатах родителя, CSS-пиксели. */
  readonly point: VizAnchorPoint;
}

/**
 * Невидимая точка, к которой цепляется Hint кита. Двигается своим layout-эффектом — он
 * срабатывает раньше эффекта Hint, и пузырь меряет уже новое место.
 */
export const VizAnchor = forwardRef<HTMLSpanElement, VizAnchorProps>(function VizAnchor(
  { point, className, ...rest },
  ref,
) {
  const own = useRef<HTMLSpanElement | null>(null);
  const setRef = useCallback(
    (node: HTMLSpanElement | null) => {
      own.current = node;
      if (typeof ref === 'function') ref(node);
      else if (ref) ref.current = node;
    },
    [ref],
  );
  useLayoutEffect(() => {
    const el = own.current;
    if (!el) return;
    const width = point.width ?? 0;
    el.style.setProperty(
      'transform',
      `translate(${(point.x - width / 2).toFixed(1)}px, ${point.y.toFixed(1)}px)`,
    );
    if (width > 0) el.style.setProperty('inline-size', `${width.toFixed(1)}px`);
    else el.style.removeProperty('inline-size');
  }, [point.x, point.y, point.width]);
  return <span ref={setRef} className={classNames('viz-anchor', className)} aria-hidden="true" {...rest} />;
});
