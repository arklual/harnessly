// Calendar days as `YYYY-MM-DD` strings, in UTC so a day never shifts with the machine's zone; Russian labels.
export const parseDay = (iso: string): Date => new Date(`${iso}T00:00:00Z`);
export const isoDay = (d: Date): string => d.toISOString().slice(0, 10);
export const addDays = (iso: string, n: number): string => {
  const d = parseDay(iso);
  d.setUTCDate(d.getUTCDate() + n);
  return isoDay(d);
};
export const daysBetween = (from: string, to: string): number =>
  Math.round((parseDay(to).getTime() - parseDay(from).getTime()) / 86_400_000);

const MONTHS = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек'];
export const monthShort = (iso: string): string => MONTHS[parseDay(iso).getUTCMonth()] ?? '';
export const dayOfMonth = (iso: string): number => parseDay(iso).getUTCDate();

/** «19–25 окт», «28 сен – 4 окт» (a range inside one month shows the month once). */
export const rangeLabel = (from: string, to: string): string => {
  const a = parseDay(from);
  const b = parseDay(to);
  if (from === to) return `${dayOfMonth(to)} ${monthShort(to)}`;
  if (a.getUTCMonth() === b.getUTCMonth()) return `${dayOfMonth(from)}–${dayOfMonth(to)} ${monthShort(to)}`;
  return `${dayOfMonth(from)} ${monthShort(from)} – ${dayOfMonth(to)} ${monthShort(to)}`;
};

/** The week strip's short label: «19–25», «31–6». */
export const weekShort = (from: string, to: string): string => `${dayOfMonth(from)}–${dayOfMonth(to)}`;

/** The current month as a word: «ноябрь». */
export const monthWord = (): string => new Date().toLocaleDateString('ru-RU', { month: 'long' });
