// SPDX-License-Identifier: Apache-2.0
/**
 * Категориальная палитра серий — акценты ДС, ступени подобраны под тёмную и светлую поверхность
 * и проверены валидатором различимости (в том числе для дальтоников). Порядок фиксирован:
 * цвет закреплён за серией, а не за её местом в рейтинге. Девятого цвета нет — хвост
 * сворачивается в «Остальные».
 */
export const VIZ_PALETTE = {
  dark: ['#8968f4', '#c8830b', '#25a897', '#e84461', '#2389e2', '#24ae56'],
  light: ['#764be5', '#c57800', '#009589', '#d31d4b', '#0072d0', '#009b44'],
} as const;

export const VIZ_OTHER_COLOR = { dark: '#7c7c86', light: '#8b8b95' } as const;

export const VIZ_PALETTE_SIZE = VIZ_PALETTE.dark.length;

export type VizTheme = keyof typeof VIZ_PALETTE;

/** Слот серии: номер в палитре или «остальные». */
export type VizColorSlot = number | 'other';

export function vizSeriesColor(slot: VizColorSlot, theme: VizTheme): string {
  if (slot === 'other') return VIZ_OTHER_COLOR[theme];
  const palette = VIZ_PALETTE[theme];
  return palette[((slot % palette.length) + palette.length) % palette.length];
}

/** Тон зоны: нейтральная полоса или подкрашенная одним из акцентов палитры. */
export const VIZ_ZONE_TONE = {
  neutral: 'neutral',
  purple: 'purple',
  amber: 'amber',
  teal: 'teal',
  rose: 'rose',
} as const;

export type VizZoneTone = (typeof VIZ_ZONE_TONE)[keyof typeof VIZ_ZONE_TONE];

const TONE_SLOT = { purple: 0, amber: 1, teal: 2, rose: 3 } as const satisfies Record<
  Exclude<VizZoneTone, 'neutral'>,
  number
>;

/** Цвет тона зоны; у нейтральной — `null`: её рисуют цветом текста с низкой прозрачностью. */
export function zoneToneColor(tone: VizZoneTone, theme: VizTheme): string | null {
  return tone === 'neutral' ? null : vizSeriesColor(TONE_SLOT[tone], theme);
}
