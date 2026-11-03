// SPDX-License-Identifier: Apache-2.0
import { DAY } from './time';
import type { VizMarker, VizMarkerPriority } from './types';

export interface VizMarkerSlot {
  readonly visible: boolean;
  /** Сколько соседей спрятано за этим значком: «+2». */
  readonly hidden: number;
}

export interface VizMarkerLayoutOptions {
  /** Пикселей на сутки при текущем масштабе. */
  readonly pxPerDay: number;
  /** Минимальный промежуток между значками, пиксели. */
  readonly minGap: number;
  /** Значок в фокусе виден всегда. */
  readonly focusId?: string | null;
  /** С какого масштаба (пикс./сутки) раскрываются приоритеты 2 и 3. */
  readonly reveal?: Readonly<Record<2 | 3, number>>;
}

const DEFAULT_REVEAL = { 2: 30, 3: 70 } as const;

/** Самый мелкий приоритет, видимый при масштабе `pxPerDay`. */
export function visiblePriority(
  pxPerDay: number,
  reveal: Readonly<Record<2 | 3, number>> = DEFAULT_REVEAL,
): VizMarkerPriority {
  if (pxPerDay >= reveal[3]) return 3;
  if (pxPerDay >= reveal[2]) return 2;
  return 1;
}

/**
 * Какие значки показать: при отдалении — только главные, с приближением раскрываются
 * второстепенные. Соседи ближе `minGap` прячутся за видимым значком, он считает «+N».
 */
export function layoutMarkers(
  markers: readonly VizMarker[],
  xOf: (t: number) => number,
  options: VizMarkerLayoutOptions,
): Map<string, VizMarkerSlot> {
  const level = visiblePriority(options.pxPerDay, options.reveal);
  const focus = options.focusId ?? null;
  const slots = new Map<string, { visible: boolean; hidden: number }>(
    markers.map((m) => [m.id, { visible: false, hidden: 0 }]),
  );
  const candidates = markers
    .filter((m) => (m.priority ?? 1) <= level || m.id === focus)
    .sort(
      (a, b) =>
        Number(b.id === focus) - Number(a.id === focus) || (a.priority ?? 1) - (b.priority ?? 1) || a.t - b.t,
    );
  const placed: { id: string; x: number }[] = [];
  for (const m of candidates) {
    const x = xOf(m.t);
    const near = placed.find((p) => Math.abs(p.x - x) < options.minGap);
    if (near) slots.get(near.id)!.hidden++;
    else {
      placed.push({ id: m.id, x });
      slots.get(m.id)!.visible = true;
    }
  }
  return slots;
}

/** Пикселей на сутки для окна `spanMs` шириной `widthPx`. */
export function pxPerDay(spanMs: number, widthPx: number): number {
  return (widthPx * DAY) / Math.max(spanMs, 1);
}
