// The clock of the day's page: the feed gives moments (RFC 3339) and the zone the person lives in, the page shows them
// in that zone and in the page's language.
import { locale, t } from '@/next/i18n';

export const PART = { am: 'am', day: 'day', pm: 'pm' } as const;
export type TPartOfDay = (typeof PART)[keyof typeof PART];

export const HOUR_MS = 3_600_000;
export const MINUTE_MS = 60_000;

export interface IDayClock {
  /** «9:30». */
  hm: (at: number) => string;
  /** The hour of the day, 0–23. */
  hour: (at: number) => number;
  /** «friday». */
  weekday: (at: number) => string;
  /** «fri 13». */
  shortDate: (at: number) => string;
  /** «13 November». */
  longDate: (at: number) => string;
  part: (at: number) => TPartOfDay;
}

const zoneOf = (tz: string | undefined): string | undefined => {
  if (tz === undefined) return undefined;
  try {
    new Intl.DateTimeFormat('en', { timeZone: tz });
    return tz;
  } catch {
    // A zone this browser does not know: the person's own one is the closest truth.
    return undefined;
  }
};

export const dayClock = (tz: string | undefined, lang: string = locale()): IDayClock => {
  const timeZone = zoneOf(tz);
  const make = (options: Intl.DateTimeFormatOptions) =>
    new Intl.DateTimeFormat(lang, { timeZone, ...options });
  const time = make({ hour: 'numeric', minute: '2-digit', hourCycle: 'h23' });
  const hours = new Intl.DateTimeFormat('en', { timeZone, hour: 'numeric', hourCycle: 'h23' });
  const weekday = make({ weekday: 'long' });
  const short = make({ weekday: 'short', day: 'numeric' });
  const long = make({ day: 'numeric', month: 'long' });
  const hour = (at: number) => Number(hours.format(at)) % 24;
  return {
    hm: (at) => time.format(at),
    hour,
    weekday: (at) => weekday.format(at),
    shortDate: (at) => short.format(at),
    longDate: (at) => long.format(at),
    part: (at) => {
      const h = hour(at);
      if (h < 12) return PART.am;
      return h < 17 ? PART.day : PART.pm;
    },
  };
};

/** «25 min», «2 h 25 min»: how long is left until a moment. */
export const leftText = (ms: number): string => {
  const minutes = Math.max(0, Math.round(ms / MINUTE_MS));
  if (minutes < 60) return t('next.time.durMinutes', { m: minutes });
  if (minutes % 60 === 0) return t('next.day.hours', { h: minutes / 60 });
  return t('next.time.durHours', { h: Math.floor(minutes / 60), m: minutes % 60 });
};

export const capitalize = (text: string): string =>
  text.charAt(0).toLocaleUpperCase(locale()) + text.slice(1);
