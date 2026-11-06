// Words and times of the environment screen.

/** 1 / 2–4 / 5+ in Russian: `plural(3, 'команда', 'команды', 'команд')`. */
export const plural = (n: number, one: string, few: string, many: string): string => {
  const a = Math.abs(n) % 100;
  const b = a % 10;
  if (a > 10 && a < 20) return many;
  if (b > 1 && b < 5) return few;
  return b === 1 ? one : many;
};

const two = (n: number): string => String(n).padStart(2, '0');

/** «сегодня 10:42», «вчера 18:12», «3 нояб. 09:00». */
export const whenText = (iso: string, now: number): string => {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return '';
  const hm = `${two(t.getHours())}:${two(t.getMinutes())}`;
  const day = (d: Date): number => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const diff = Math.round((day(new Date(now)) - day(t)) / 86_400_000);
  if (diff === 0) return `сегодня ${hm}`;
  if (diff === 1) return `вчера ${hm}`;
  const months = [
    'янв.',
    'февр.',
    'марта',
    'апр.',
    'мая',
    'июня',
    'июля',
    'авг.',
    'сент.',
    'окт.',
    'нояб.',
    'дек.',
  ];
  return `${t.getDate()} ${months[t.getMonth()] ?? ''} ${hm}`;
};

/** 9000 → «9 с»; under a second «<1 с». */
export const secs = (ms: number): string => (ms < 1000 ? '<1 с' : `${Math.round(ms / 1000)} с`);
