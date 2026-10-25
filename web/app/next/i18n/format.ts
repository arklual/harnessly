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
