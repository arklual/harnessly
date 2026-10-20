import type { IEffFamily } from '@/types/efficiency';
import { locale, t } from '@/lib/i18n';

export type TFamilySort = 'carry' | 'calls' | 'errors';

export type TTone = 'bad' | 'warn' | 'ok' | 'flat';

export const FAMILY_SORTS: { value: TFamilySort; label: string }[] = [
  { value: 'carry', label: t('efficiency.familiesMath.byCarry') },
  { value: 'calls', label: t('efficiency.familiesMath.byCalls') },
  { value: 'errors', label: t('efficiency.familiesMath.byErrors') },
];

/** Families too small to judge an error rate by: one failed call out of five is not a trend. */
const MIN_CALLS_FOR_RATE = 30;

export const sortFamilies = (families: IEffFamily[], by: TFamilySort): IEffFamily[] => {
  const key = {
    carry: (f: IEffFamily) => f.carryTokens,
    calls: (f: IEffFamily) => f.calls,
    errors: (f: IEffFamily) => (f.calls >= MIN_CALLS_FOR_RATE ? f.errRate : -1),
  }[by];
  return [...families].sort(
    (a, b) => key(b) - key(a) || b.calls - a.calls || a.name.localeCompare(b.name, locale()),
  );
};

export const errorTone = (family: Pick<IEffFamily, 'errRate' | 'calls'>): TTone => {
  if (family.calls < MIN_CALLS_FOR_RATE) return 'flat';
  if (family.errRate >= 0.06) return 'bad';
  if (family.errRate >= 0.035) return 'warn';
  return 'flat';
};

/** Width of a bar in percent of the longest one; a non-zero value never vanishes. */
export const barWidths = (values: number[]): number[] => {
  const max = Math.max(...values, 0);
  if (max <= 0) return values.map(() => 0);
  return values.map((v) => (v <= 0 ? 0 : Math.max(1.5, (v / max) * 100)));
};

export interface ISparkBar {
  x: number;
  y: number;
  height: number;
}

/** Bars of the per-day sparkline inside a viewBox of width × height. */
export const sparkBars = (series: number[], width: number, height: number): ISparkBar[] => {
  const max = Math.max(...series, 1);
  const step = width / Math.max(series.length, 1);
  return series.flatMap((v, i) =>
    v > 0 ? [{ x: i * step, y: height - (v / max) * height, height: (v / max) * height }] : [],
  );
};
