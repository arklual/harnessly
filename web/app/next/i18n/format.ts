// Times and durations in words, in the page's language.
import { locale, t } from '@/next/i18n';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
export const DAY = 24 * HOUR;

/** Midnight of the day `now` falls in, by the device's clock. */
export const startOfDay = (now: number) => {
  const start = new Date(now);
  start.setHours(0, 0, 0, 0);
  return start.getTime();
};

/** A short «when» for a list: now, minutes, hours, yesterday, then the date. */
export const formatAgo = (at: number, now: number): string => {
  const ago = Math.max(0, now - at);
  if (ago < MINUTE) return t('next.time.now');
  if (ago < HOUR) return t('next.time.minutes', { n: Math.floor(ago / MINUTE) });
  const today = startOfDay(now);
  if (at >= today) return t('next.time.hours', { n: Math.floor(ago / HOUR) });
  if (at >= today - DAY) return t('next.time.yesterday');
  return new Intl.DateTimeFormat(locale(), { day: 'numeric', month: 'short' }).format(at);
};

/** The card's «when»: as formatAgo, with «назад» after minutes and hours («2 ч назад»). */
export const formatAgoFull = (at: number, now: number): string => {
  const ago = Math.max(0, now - at);
  const short = formatAgo(at, now);
  if (ago < MINUTE || at < startOfDay(now)) return short;
  return t('next.time.agoFull', { when: short });
};

/** «5 min», «3 h», «2 d»: how old something is, to its biggest unit. */
export const age = (ms: number): string => {
  if (ms < MINUTE) return t('next.time.now');
  if (ms < HOUR) return t('next.time.minutes', { n: Math.floor(ms / MINUTE) });
  if (ms < DAY) return t('next.time.hours', { n: Math.floor(ms / HOUR) });
  return t('next.time.days', { n: Math.floor(ms / DAY) });
};

/** «4 min 12 s» / «12 s»: how long something has been going, for the live turn's timer. */
export const elapsed = (ms: number): string => {
  const total = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  if (minutes >= 60) return t('next.time.durHours', { h: Math.floor(minutes / 60), m: minutes % 60 });
  if (minutes > 0) return t('next.time.minutesSeconds', { m: minutes, s: seconds });
  return t('next.time.seconds', { s: seconds });
};

/** «4 min» or «40 s»: a finished turn's length in the folded line. */
export const duration = (ms: number): string => {
  const total = Math.max(0, Math.round(ms / 1000));
  if (total < 60) return t('next.time.seconds', { s: total });
  const minutes = Math.round(total / 60);
  if (minutes >= 60) return t('next.time.durHours', { h: Math.floor(minutes / 60), m: minutes % 60 });
  return t('next.time.durMinutes', { m: minutes });
};

/** «16:50»: a clock time of the person's day. */
export const clock = (at: number): string =>
  new Intl.DateTimeFormat(locale(), { hour: '2-digit', minute: '2-digit' }).format(at);

/** A moment ahead to plan by: «18:00» today, «пн 09:00» within the week, «26 окт, 09:00» later; `dated` for the last. */
export const moment = (at: number, now: number): { text: string; dated: boolean } => {
  const today = startOfDay(now);
  if (at < today + DAY) return { text: clock(at), dated: false };
  if (at < today + 7 * DAY) {
    const day = new Intl.DateTimeFormat(locale(), { weekday: 'short' }).format(at);
    return { text: `${day} ${clock(at)}`, dated: false };
  }
  const date = new Intl.DateTimeFormat(locale(), { day: 'numeric', month: 'short' }).format(at);
  return { text: `${date}, ${clock(at)}`, dated: true };
};

/** How far back a time is, for a list grouped by it: today, yesterday, the six days before, this month, earlier. The
 *  week is the sidebar's (the seven days up to today), so a weekday names one day only. */
export const WHEN = {
  today: 'today',
  yesterday: 'yesterday',
  week: 'week',
  month: 'month',
  older: 'older',
} as const;
export type TWhen = (typeof WHEN)[keyof typeof WHEN];

export const whenOf = (at: number, now: number): TWhen => {
  const today = startOfDay(now);
  if (at >= today) return WHEN.today;
  if (at >= today - DAY) return WHEN.yesterday;
  if (at >= today - 6 * DAY) return WHEN.week;
  const first = new Date(now);
  first.setDate(1);
  first.setHours(0, 0, 0, 0);
  if (at >= first.getTime()) return WHEN.month;
  return WHEN.older;
};

/** A list row's «when», one form everywhere in the search: «сейчас», «12 мин», «3 ч» today, «вчера», «ср» within the
 *  week, «5 нояб.» this year, «12 окт. 2025» before it. */
export const formatWhen = (at: number, now: number): string => {
  const when = whenOf(at, now);
  if (when === WHEN.today || when === WHEN.yesterday) return formatAgo(at, now);
  if (when === WHEN.week) return new Intl.DateTimeFormat(locale(), { weekday: 'short' }).format(at);
  const date = new Intl.DateTimeFormat(locale(), { day: 'numeric', month: 'short' }).format(at);
  const year = new Date(at).getFullYear();
  return year === new Date(now).getFullYear() ? date : `${date} ${year}`;
};

const KB = 1024;
const MB = 1024 * KB;

/** «4 КБ», «1,2 МБ»: a size of text for a person, to a whole KB (at least 1) or a tenth of a MB. */
export const size = (bytes: number): string => {
  if (bytes >= MB) {
    const n = new Intl.NumberFormat(locale(), { maximumFractionDigits: 1 }).format(bytes / MB);
    return t('next.size.mb', { n });
  }
  return t('next.size.kb', { n: Math.max(1, Math.round(bytes / KB)) });
};

/** Under an output cut by its size: «первые 4 КБ из 120 КБ», not «Показано 4096 из 122880». */
export const cutBySize = (shown: number, total: number): string =>
  t('next.tool.firstBytes', { shown: size(shown), total: size(total) });
