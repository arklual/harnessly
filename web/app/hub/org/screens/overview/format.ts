import { inRubles, shownOfUsd, shownSymbol } from '../../money';

// Number and time texts of Обзор, in Russian (decimal comma, «тыс», «млн»).
const dec = (n: number, digits = 1, fixed = false): string =>
  n.toLocaleString('ru-RU', { maximumFractionDigits: digits, minimumFractionDigits: fixed ? digits : 0 });

/** 312000 → «312 тыс ₽»; 10400 → «10,4 тыс ₽»; 850 → «850 ₽». */
export const rub = (n: number): string => {
  if (n >= 1_000_000) return `${dec(n / 1_000_000)} млн ${shownSymbol()}`;
  if (n >= 1000) return `${dec(n / 1000)} тыс ${shownSymbol()}`;
  return `${dec(n, 0)} ${shownSymbol()}`;
};

/** Thousands only, without the currency: «12,7 тыс». */
export const thousands = (n: number): string => (inRubles() ? `${dec(n / 1000)} тыс` : dec(n, 0));

export const usd = (n: number): string => `$${dec(n, n < 10 ? 1 : 0)}`;

/**
 * A dollar amount in the hub's currency, compact: $4,2 → «378 ₽», $810 → «72,9 тыс ₽». Every screen writes money with
 * this, so a page never mixes ₽ and $ (HRN-670); dollars only while the hub has not said its rate.
 */
export const fromUsd = (n: number): string => {
  const s = shownOfUsd(n);
  if (s === null) return usd(n);
  return inRubles() ? rub(s) : usd(s);
};

/** The same, whole and exact: $18,5 → «1 665 ₽» (a KPI, a price per MR). */
export const fromUsdExact = (n: number): string => {
  const s = shownOfUsd(n);
  if (s === null) return usd(n);
  return inRubles() ? `${dec(s, 0)} ₽` : usd(s);
};

export const tokens = (n: number): string => {
  if (n >= 1_000_000_000) return `${dec(n / 1_000_000_000)} млрд`;
  if (n >= 1_000_000) return `${dec(n / 1_000_000, 0)} млн`;
  if (n >= 1000) return `${dec(n / 1000, 0)} тыс`;
  return dec(n, 0);
};

export const num = dec;

const days = new Intl.PluralRules('ru');
const dayWord = (n: number): string => {
  const f = days.select(n);
  return f === 'one' ? 'день' : f === 'few' ? 'дня' : 'дней';
};

/** A moment relative to now: «сейчас», «12 мин назад», «3 ч назад», «вчера», «2 дня назад». */
export const ago = (iso: string | null, now: number): string => {
  if (!iso) return 'сейчас';
  const diff = Math.max(0, now - Date.parse(iso));
  const min = Math.floor(diff / 60_000);
  if (min < 1) return 'сейчас';
  if (min < 60) return `${min} мин назад`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h} ч назад`;
  const d = Math.floor(h / 24);
  return d === 1 ? 'вчера' : `${d} ${dayWord(d)} назад`;
};

/** How long a person has been away: «9 дней не заходит». */
export const awayFor = (iso: string, now: number): string | null => {
  const d = Math.floor((now - Date.parse(iso)) / 86_400_000);
  return d >= 7 ? `${d} ${dayWord(d)} не заходит` : null;
};

/** «1 ноября, завтра»: a reset date in words. */
export const resetText = (iso: string, now: number): string => {
  const at = new Date(iso);
  const date = at.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' });
  const d = Math.round((at.getTime() - now) / 86_400_000);
  const rel = d <= 0 ? 'сегодня' : d === 1 ? 'завтра' : `через ${d} ${dayWord(d)}`;
  return `${date}, ${rel}`;
};

export const toneOf = (pct: number): 'ok' | 'warn' | 'bad' => (pct >= 80 ? 'bad' : pct >= 60 ? 'warn' : 'ok');

export const ESTIMATE_NOTE =
  'Расход — оценка по ценам моделей: у этих аккаунтов нет месячной платы. Денежные лимиты на них пока не действуют, потому что организация их не оплачивает.';
