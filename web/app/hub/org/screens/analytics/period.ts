// The period and its comparison are chosen in whole weeks. By default the comparison is the same number of weeks
// right before the period.
import type { AnalyticsFrame, ChangeRef, Week } from '../../api/analytics';
import { addDays, daysBetween, parseDay } from '../../dates';

export const spanOf = (weeks: readonly Week[], a: number, b: number): Week => {
  const lo = weeks[Math.min(a, b)];
  const hi = weeks[Math.max(a, b)];
  if (!lo || !hi) throw new Error('week index out of range');
  return { from: lo.from, to: hi.to };
};

export const weekCount = (w: Week): number => Math.round((daysBetween(w.from, w.to) + 1) / 7);

/** The same number of weeks right before. */
export const defaultCompare = (period: Week): Week => {
  const days = daysBetween(period.from, period.to) + 1;
  return { from: addDays(period.from, -days), to: addDays(period.from, -1) };
};

export const sameWeek = (a: Week, b: Week): boolean => a.from === b.from && a.to === b.to;

export interface Preset {
  id: string;
  label: string;
  period: Week;
  compare: Week;
}

/** The ready choices over the last full weeks (the current, unfinished week is the last one and is not used). */
export const presetsOf = (frame: AnalyticsFrame): Preset[] => {
  const w = frame.weeks;
  const n = w.length;
  const out: Preset[] = [];
  if (n >= 4) {
    out.push({
      id: 'last-week',
      label: 'Прошлая неделя против позапрошлой',
      period: spanOf(w, n - 2, n - 2),
      compare: spanOf(w, n - 3, n - 3),
    });
  }
  if (n >= 10) {
    out.push({
      id: 'four-weeks',
      label: '4 недели против 4 до них',
      period: spanOf(w, n - 5, n - 2),
      compare: spanOf(w, n - 9, n - 6),
    });
  }
  const change: ChangeRef | null = frame.lastChange;
  if (change) {
    const after = w.findIndex((x) => parseDay(x.from).getTime() > parseDay(change.at).getTime());
    if (after >= 1 && after < n) {
      out.push({
        id: 'after-change',
        label: `Неделя после «${change.title}» против недели до`,
        period: spanOf(w, after, after),
        compare: spanOf(w, after - 1, after - 1),
      });
    }
  }
  return out;
};
