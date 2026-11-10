// The words and numbers of «Обзор», written the way the board writes them (decimal comma, «тыс», «млн», «млрд»).
import type { ChatEnd } from '../../../org/api/member';
import type { OverviewPeriod } from '../../../org/api/overview';
import { monthWord } from '../../../org/dates';
import { inRubles, shownSymbol } from '../../../org/money';
import { ago, fromUsdExact, rub, tokens, usd } from '../../../org/screens/overview/format';
import { kRub, plural, thousand } from '../../../org/screens/teams/format';

export { ago, kRub, plural, rub, thousand, tokens, usd };

const dec = (n: number, digits = 1): string =>
  n.toLocaleString('ru-RU', { maximumFractionDigits: digits, minimumFractionDigits: 0 });

/** «одна» decimal digit always: «$4,2», «3,0». */
export const fixed1 = (n: number): string =>
  n.toLocaleString('ru-RU', { maximumFractionDigits: 1, minimumFractionDigits: 1 });

// ── The period ──────────────────────────────────────────────────────────────────────────────────────────────────

export const PERIODS: { value: OverviewPeriod; label: string }[] = [
  { value: 'week', label: 'Неделя' },
  { value: 'month', label: 'Месяц' },
];

/** «октябрь» for the month, «неделя» for the week. */
export const periodWord = (p: OverviewPeriod): string => (p === 'week' ? 'неделя' : monthWord());

/** «за неделю» / «за ноябрь». */
export const forPeriod = (p: OverviewPeriod): string => (p === 'week' ? 'за неделю' : `за ${monthWord()}`);

// ── The donut ───────────────────────────────────────────────────────────────────────────────────────────────────

export type Unit = 'rub' | 'usd' | 'tok';

/** The units the donut counts in: money in the shown currency and tokens; one currency on the whole hub (HRN-670). */
export const unitsOf = (): { id: Unit; label: string }[] =>
  inRubles()
    ? [
        { id: 'rub', label: shownSymbol() },
        { id: 'tok', label: 'Токены' },
      ]
    : [
        { id: 'usd', label: '$' },
        { id: 'tok', label: 'Токены' },
      ];

/** 1460000000 → «1,46 млрд», 410000000 → «410 млн», 12000 → «12 тыс». */
export const tokensLong = (n: number): string => {
  if (n >= 1_000_000_000) return `${dec(n / 1_000_000_000, 2)} млрд`;
  if (n >= 1_000_000) return `${dec(n / 1_000_000, 0)} млн`;
  if (n >= 1000) return `${dec(n / 1000, 0)} тыс`;
  return dec(n, 0);
};

/** 118000 → «118 тыс ₽», 1300 (dollars) → «$1 300». */
export const unitValue = (unit: Unit, v: { rub: number; usd: number; tokens: number }): string => {
  if (unit === 'tok') return tokensLong(v.tokens);
  if (unit === 'usd') return `$${v.usd.toLocaleString('ru-RU')}`;
  return rub(v.rub);
};

export const unitCaption = (unit: Unit, p: OverviewPeriod): string =>
  unit === 'tok' ? `токенов ${forPeriod(p)}` : forPeriod(p);

// ── Tiles ───────────────────────────────────────────────────────────────────────────────────────────────────────

/** 12700 → «12,7»: the thousands of a sum whose «тыс ₽» stands further on («12,7 из 30 тыс ₽»); dollars stay whole. */
export const thousandBare = (n: number): string => thousand(n).replace(/ тыс$/, '');

export const people = (n: number): string => `${n} ${plural(n, 'человек', 'человека', 'человек')}`;

/** «10,4 тыс ₽ · 48 млн токенов»: the money once, in the hub's currency. */
export const spendLine = (s: { rub: number; usd: number; tokens: number }, estimated: boolean): string => {
  const parts = [inRubles() ? rub(s.rub) : usd(s.usd), `${tokens(s.tokens)} токенов`];
  return `${parts.join(' · ')}${estimated ? ' · оценка' : ''}`;
};

export const activeLine = (now: number, total: number): string => `${people(now)} из ${total}`;

export const tasksLine = (closed: number, inProgress: number): string =>
  `${closed} закрыто · ${inProgress} в работе`;

export const accountsLine = (accounts: number, worstName: string, worstPct: number): string =>
  accounts === 0
    ? 'общих аккаунтов нет'
    : `${accounts} ${plural(accounts, 'общий аккаунт', 'общих аккаунта', 'общих аккаунтов')} · ближе всех к лимиту ${worstName} — ${dec(worstPct, 0)}%`;

/** What the head tiles are called: «сегодня» when the hub counts the day, otherwise the period. */
export const tileLabels = (basis: 'today' | 'period', p: OverviewPeriod) =>
  basis === 'today'
    ? {
        spend: 'Расход сегодня',
        active: 'Активны сегодня',
        tasks: 'Задачи сегодня',
        nav: 'Сводка за сегодня',
        scope: 'Считаем за сегодня.',
      }
    : {
        spend: `Расход ${forPeriod(p)}`,
        active: `Активны ${forPeriod(p)}`,
        tasks: `Задачи ${forPeriod(p)}`,
        nav: `Сводка ${forPeriod(p)}`,
        scope: `Считаем ${forPeriod(p)}.`,
      };

// ── Efficiency and subscriptions ────────────────────────────────────────────────────────────────────────────────

/** «8,1 задачи на чел.»: a fraction takes «задачи», a whole number is declined. */
export const perPersonLong = (v: number): string => {
  const whole = Number.isInteger(v);
  return `${dec(v)} ${whole ? plural(v, 'задача', 'задачи', 'задач') : 'задачи'} на чел.`;
};

/** The one decimal always: «3,0». */
export const perPersonShort = (v: number): string => fixed1(v);

export const reachedLong = (pct: number | null): string =>
  pct === null ? 'мало чатов, чтобы судить' : `${pct}% чатов дошли до результата`;
export const reachedShort = (pct: number | null): string => (pct === null ? 'мало чатов' : `${pct}% чатов`);

// ── A lead's people ─────────────────────────────────────────────────────────────────────────────────────────────

/** «из 20 тыс · 93%» under the spend; «без личного лимита» when none is set. */
export const limitLine = (limit: number | null, share: number): string =>
  limit === null
    ? 'без личного лимита'
    : `из ${inRubles() ? thousand(limit) : `${shownSymbol()}${thousand(limit)}`} · ${Math.round(share * 100)}%`;

const DAY_MS = 86_400_000;
const days = (n: number) => plural(n, 'день', 'дня', 'дней');

/** «лид · 3 ч назад», «вчера», «9 дней не заходит», «приглашена, ещё не зашла»; nothing known: just the role. */
export const whoSub = (
  p: { isLead: boolean; lastActiveAt: string | null; invited: boolean },
  now: number,
): string => {
  if (p.invited) return 'приглашена, ещё не зашла';
  const lead = p.isLead ? 'лид' : '';
  if (!p.lastActiveAt) return lead;
  const d = Math.floor((now - Date.parse(p.lastActiveAt)) / DAY_MS);
  const since = d >= 7 ? `${d} ${days(d)} не заходит` : ago(p.lastActiveAt, now);
  return lead ? `${lead} · ${since}` : since;
};

// ── A participant ───────────────────────────────────────────────────────────────────────────────────────────────

const FLEETING: Record<string, string> = {
  Павел: 'Павла',
  Пётр: 'Петра',
  Петр: 'Петра',
  Лев: 'Льва',
  Любовь: 'Любови',
};

/**
 * «Аня» → «Ани», «Олег» → «Олега»: the genitive of one Russian first name, for «попросить у …». Names the rules do not
 * cover with certainty (several words, Latin letters, an ending out of the common ones) come back as null, and the
 * caller writes a role instead of a wrong form.
 */
export const genitive = (name: string): string | null => {
  const n = name.trim();
  if (!/^[А-ЯЁ][а-яё]+$/.test(n)) return null;
  const known = FLEETING[n];
  if (known) return known;
  const stem = n.slice(0, -1);
  const last = n.slice(-1);
  const before = n.slice(-2, -1);
  if (last === 'а') return `${stem}${'гкхжшчщ'.includes(before) ? 'и' : 'ы'}`;
  if (last === 'я') return `${stem}и`;
  if (last === 'й') return `${stem}я`;
  if (last === 'ь') return `${stem}я`;
  if ('бвгджзклмнпрстфхцчшщ'.includes(last)) return `${n}а`;
  return null;
};

/** «1 ноября»: the date of a reset. */
export const dayMonth = (iso: string): string =>
  new Date(iso).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long' });

export const chatsLine = (week: number, running: number): string =>
  running > 0 ? `${week} · ${running} ${plural(running, 'идёт', 'идут', 'идут')} сейчас` : `${week}`;

export const mrLine = (week: number, merged: number | null): string =>
  merged === null
    ? `${week}`
    : `${week} · ${merged} ${plural(merged, 'уже смёржен', 'уже смёржены', 'уже смёржены')}`;

export const machineLine = (m: { name: string; online: boolean; loadPct: number | null }): string =>
  `${m.name} ${m.online ? 'на связи' : 'не в сети'}${m.online && m.loadPct !== null ? ` · загрузка ${Math.round(m.loadPct)}%` : ''}`;

export type ChatMark = 'run' | 'ok' | 'rev' | 'drop';

export const CHAT_MARK: Record<ChatEnd, ChatMark> = {
  running: 'run',
  merged: 'ok',
  review: 'rev',
  abandoned: 'drop',
};

export const chatTag = (end: ChatEnd, mr: string | null): string =>
  end === 'running'
    ? 'идёт'
    : end === 'merged'
      ? `MR ${mr ?? ''} смёржен`.replace('  ', ' ')
      : end === 'review'
        ? `MR ${mr ?? ''} на ревью`.replace('  ', ' ')
        : 'брошен без MR';

/** «378 ₽»: a chat's cost in the hub's currency. */
export const chatCost = (usdAmount: number): string => fromUsdExact(usdAmount);

// ── The lead line under the title ───────────────────────────────────────────────────────────────────────────────

/** «Тенант Продукт · октябрь», «Команда Фронт · неделя», «Только твоя работа · октябрь». */
export const overviewLead = (
  role: 'admin' | 'lead' | 'member',
  scope: { tenantName: string | null; teamName: string | null },
  period: OverviewPeriod,
): string => {
  const p = periodWord(period);
  if (role === 'member') return `Только твоя работа · ${p}`;
  if (role === 'lead') return scope.teamName ? `Команда ${scope.teamName} · ${p}` : `Твоя команда · ${p}`;
  return scope.tenantName ? `Тенант ${scope.tenantName} · ${p}` : `Вся организация · ${p}`;
};
