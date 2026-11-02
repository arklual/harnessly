// How the analytics numbers are written (Russian: decimal comma, «2 ч 10 мин», «23K») and how a change is told.
import type { Kpi, KpiFormat } from '../../api/analytics';

const num = (n: number, digits: number): string =>
  new Intl.NumberFormat('ru-RU', { minimumFractionDigits: 0, maximumFractionDigits: digits }).format(n);

export const minutesText = (m: number): string => {
  const total = Math.round(m);
  if (total < 60) return `${total} мин`;
  const h = Math.floor(total / 60);
  const rest = total % 60;
  return rest === 0 ? `${h} ч` : `${h} ч ${rest} мин`;
};

export const formatValue = (format: KpiFormat, v: number | null): string => {
  if (v === null) return '—';
  switch (format) {
    case 'usd':
      return `$${num(v, v < 100 ? 1 : 0)}`;
    case 'pct':
      return `${num(v, v < 10 ? 1 : 0)}%`;
    case 'minutes':
      return minutesText(v);
    case 'count':
      return num(v, 1);
    case 'tokens':
      return v >= 1000 ? `${num(v / 1000, 0)}K` : num(v, 0);
    case 'perMonth':
    case 'perWeek':
      return num(v, 1);
  }
};

/** «было $21,0» — the old value as the tile's last line. */
export const wasText = (kpi: Kpi): string => {
  if (kpi.prev === null) return 'раньше данных не было';
  const v = kpi.format === 'usd' ? `$${num(kpi.prev, 1)}` : formatValue(kpi.format, kpi.prev);
  if (kpi.format === 'perMonth') return `было ${v} в месяц`;
  if (kpi.format === 'perWeek') return `было ${v} в неделю`;
  if (kpi.format === 'tokens') return `было ${v} токенов`;
  if (kpi.format === 'count') return `было ${v} сессии`;
  return `было ${v}`;
};

export type DeltaTone = 'good' | 'bad' | 'flat';
export interface DeltaText {
  tone: DeltaTone;
  /** «↓ 12%», «↑ 5 п.»; empty when the change is within noise. */
  text: string;
}

/** The change since the compare period; inside the noise it is «как раньше» (tone flat, no arrow). */
export const deltaOf = (kpi: Kpi): DeltaText => {
  if (kpi.value === null || kpi.prev === null) return { tone: 'flat', text: '' };
  const diff = kpi.value - kpi.prev;
  const size =
    kpi.delta === 'rel' ? (kpi.prev === 0 ? 0 : (Math.abs(diff) / Math.abs(kpi.prev)) * 100) : Math.abs(diff);
  if (size < kpi.noise) return { tone: 'flat', text: '' };
  const arrow = diff > 0 ? '↑' : '↓';
  const good = kpi.better === 'up' ? diff > 0 : diff < 0;
  const body =
    kpi.delta === 'rel' ? `${num(size, 0)}%` : kpi.delta === 'pts' ? `${num(size, 1)} п.` : num(size, 1);
  return { tone: good ? 'good' : 'bad', text: `${arrow} ${body}` };
};
