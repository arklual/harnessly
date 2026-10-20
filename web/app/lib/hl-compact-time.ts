import { t } from '@/lib/i18n';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const MONTH = 30 * DAY;
const YEAR = 365 * DAY;

const STEPS = [
  { below: HOUR, size: MINUTE, key: 'lib.hlCompactTime.minutes' },
  { below: DAY, size: HOUR, key: 'lib.hlCompactTime.hours' },
  { below: WEEK, size: DAY, key: 'lib.hlCompactTime.days' },
  { below: MONTH, size: WEEK, key: 'lib.hlCompactTime.weeks' },
  { below: YEAR, size: MONTH, key: 'lib.hlCompactTime.months' },
  { below: Infinity, size: YEAR, key: 'lib.hlCompactTime.years' },
] as const;

/** True when compactAge() says "now" (under a minute): callers word "just now" instead of "now ago". */
export const compactAgeIsNow = (at: number, now: number): boolean => Math.max(0, now - at) < MINUTE;

/** Age of a conversation for the sidebar column, as in Codex: "now", "8m", "3h", "1d", "2w". */
export const compactAge = (at: number, now: number): string => {
  const age = Math.max(0, now - at);
  if (age < MINUTE) return t('lib.hlCompactTime.now');
  const step = STEPS.find((candidate) => age < candidate.below)!;
  return t(step.key, { n: Math.floor(age / step.size) });
};

/** Short form of a machine name for the row tag: "Runner 2" → "R2", "dev-department" → "dev". */
export const machineTag = (name: string): string => {
  const words = name.split(/[\s._-]+/).filter(Boolean);
  const digits = words.find((word) => /^\d+$/.test(word));
  if (digits && words[0] !== digits) return `${words[0][0].toUpperCase()}${digits}`;
  return (words[0] ?? name).slice(0, 4);
};
