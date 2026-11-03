// The numbers behind the board's small charts, as pure functions: the bars of a tile, the line of a load row, the
// shares of a strip. The components in this folder only draw what these return.

export interface BarRect {
  x: number;
  y: number;
  width: number;
  height: number;
  recent: boolean;
}

const BAR_W = 5.14;
const BAR_STEP = 100 / 14;
const BAR_TOP = 2;
const BAR_MIN = 3;
const BAR_BOX = 40;

/** Up to 14 bars in a 100×40 box: the tallest takes 38, the smallest never less than 3, the last one is «recent». */
export const barRects = (values: readonly number[]): BarRect[] => {
  const vals = values.slice(-14);
  const max = Math.max(1, ...vals);
  return vals.map((v, i) => {
    const height = Math.max(BAR_MIN, ((BAR_BOX - BAR_TOP) * Math.max(0, v)) / max);
    return { x: 1 + i * BAR_STEP, y: BAR_BOX - height, width: BAR_W, height, recent: i === vals.length - 1 };
  });
};

/** The line of a 24-point load history in a 100×24 box: 0% sits at the bottom, 100% two units from the top. */
export const sparkPath = (values: readonly number[]): string => {
  if (values.length === 0) return '';
  const last = Math.max(1, values.length - 1);
  return values
    .map((v, i) => {
      const x = ((i / last) * 100).toFixed(1);
      const y = (24 - (Math.max(0, Math.min(100, v)) / 100) * 22).toFixed(1);
      return `${i === 0 ? 'M' : 'L'}${x} ${y}`;
    })
    .join(' ');
};

/** A load at or above this share is drawn hot. */
export const HOT_PCT = 85;

export const isHot = (pct: number | null | undefined): boolean => pct != null && pct >= HOT_PCT;

/** «34%», or a dash for no data and for zero (the board draws an idle meter as «—»). */
export const pctText = (pct: number | null | undefined): string => (pct != null && pct > 0 ? `${pct}%` : '—');

/** The width of a part of a whole in percent, one decimal: `--w` of a bar and of a meter. */
export const share = (n: number, total: number): string =>
  `${total > 0 ? Math.max(0, Math.min(100, (n / total) * 100)).toFixed(1) : '0.0'}%`;
