// SPDX-License-Identifier: Apache-2.0
import type { VizColorSlot, VizZoneTone } from './palette';
import type { VizGranularity, VizTimeRange } from './time';

/**
 * Серия временного графика. `Id` — литеральный тип ключей продукта (`'dance' | 'vip'`), чтобы
 * легенда, данные и подсветка не разъезжались по опечатке.
 */
export interface VizSeries<Id extends string = string> {
  readonly id: Id;
  /** Подпись в легенде и тултипе — данные продукта, выводится как текст. */
  readonly label: string;
  /** Место в палитре. По умолчанию — порядковый номер серии. */
  readonly slot?: VizColorSlot;
}

/**
 * Значения серий по отрезкам одной детализации: что отдаёт бэкенд на запрос окна.
 * `NaN` — пропуск (данных за отрезок нет), а не ноль: ноль — это «продаж не было».
 */
export interface VizBucketFrame<Id extends string = string> {
  readonly granularity: VizGranularity;
  /** Начало первого отрезка, мс UTC. */
  readonly start: number;
  /** Длина отрезка, мс. */
  readonly step: number;
  /** Число отрезков. */
  readonly length: number;
  readonly values: Readonly<Record<Id, ArrayLike<number>>>;
}

/** Границы всей истории графика: что показывает полоса «весь период». */
export interface VizTimeExtent extends VizTimeRange {
  /** «Сейчас»: правее — будущее, данных там нет. */
  readonly now: number;
  /** Момент мероприятия, если есть: полоса показывает дни до него. */
  readonly event?: number;
}

/** Зона — период на оси времени: волна цены, рекламная кампания, своя отметка. */
export interface VizZone {
  readonly id: string;
  readonly from: number;
  readonly to: number;
  readonly label: string;
  /** Вторая, приглушённая часть подписи: «танцпол 2 500 ₽». */
  readonly note?: string;
  readonly tone?: VizZoneTone;
  /** Своя зона пользователя: у подписи крестик удаления. */
  readonly removable?: boolean;
}

/**
 * Приоритет значка: 1 — видно всегда (старт, цены), 2 — при приближении (промокоды, реклама),
 * 3 — только вблизи (посты, напоминания).
 */
export type VizMarkerPriority = 1 | 2 | 3;

/** Значок изменения — что сделал организатор в момент `t`. */
export interface VizMarker<Id extends string = string, Kind extends string = string> {
  readonly id: string;
  readonly t: number;
  /** Вид изменения: по нему продукт выбирает иконку. */
  readonly kind: Kind;
  readonly label: string;
  readonly detail?: string;
  /** Серии, которых изменение касалось; пусто — всех. */
  readonly series?: readonly Id[];
  readonly priority?: VizMarkerPriority;
}

/** Как серии складываются: наложением, стопкой или долями от суммы. */
export const VIZ_STACK_MODE = {
  overlay: 'overlay',
  stack: 'stack',
  percent: 'percent',
} as const;

export type VizStackMode = (typeof VIZ_STACK_MODE)[keyof typeof VIZ_STACK_MODE];

/** Какие точки рисовать на линиях. */
export const VIZ_POINTS = {
  auto: 'auto',
  always: 'always',
  none: 'none',
} as const;

export type VizPoints = (typeof VIZ_POINTS)[keyof typeof VIZ_POINTS];
