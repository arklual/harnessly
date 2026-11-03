// SPDX-License-Identifier: Apache-2.0
import type { VizTimeEngine } from './time-engine';

/** Точка в координатах холста, CSS-пиксели. */
export interface VizPoint {
  readonly x: number;
  readonly y: number;
  /**
   * Ширина того, на что указывает подсказка (столбик), с центром в `x`. Пузырь встаёт сбоку от
   * неё и не закрывает наведённый столбик. Нет — точка.
   */
  readonly width?: number;
}

/**
 * Как график рисует данные: волны, столбики. Всё остальное — окно, слои, стопка, оси, зоны,
 * значки, ввод и подсказки — общее у всех графиков времени и живёт в `VizTimeEngine`.
 */
export interface VizChartRenderer {
  readonly kind: 'wave' | 'bar';
  /**
   * Смена детализации: `blend` — кривые перетекают во времени из старой в новую,
   * `aggregate` — столбики делятся на части (день → часы) или сливаются (часы → день).
   */
  readonly morph: 'blend' | 'aggregate';
  /** Рисует кадр целиком. Возвращает, нужен ли следующий кадр. */
  paint(engine: VizTimeEngine, dt: number, now: number): boolean;
  /** Серия под точкой холста. */
  hit(engine: VizTimeEngine, x: number, y: number): string | null;
  /**
   * Куда указывает подсказка отрезка `k`: на серию `seriesId` или на верх отрезка.
   * `follow` — следовать за плавно едущим перекрестьем, а не стоять ровно на отрезке.
   */
  anchor(engine: VizTimeEngine, k: number, seriesId: string | null, follow: boolean): VizPoint | null;
}
