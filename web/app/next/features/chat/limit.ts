// What a chat held on its agent's usage limit says: the title, when it goes on, how long that is. Plain functions of the
// hold and the machine's now, so the card, the field and the sidebar say the same and a test can read them.
import { moment } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import type { LimitHold, Provider } from '@/next/protocol/v2';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

export const PROVIDER_NAME = { claude: 'Claude', codex: 'Codex' } as const satisfies Record<Provider, string>;

/** What the runtime will do with the hold: go on by itself, wait for the person, or has no time to go by. */
export const LIMIT_PHASE = { auto: 'auto', held: 'held', unknown: 'unknown' } as const;
export type TLimitPhase = (typeof LIMIT_PHASE)[keyof typeof LIMIT_PHASE];

export const limitPhase = (hold: Pick<LimitHold, 'resetsAt' | 'continueAt'>): TLimitPhase => {
  if (hold.continueAt !== null) return LIMIT_PHASE.auto;
  return hold.resetsAt === null ? LIMIT_PHASE.unknown : LIMIT_PHASE.held;
};

/** «в 18:00», «в пн 09:00», «26 окт, 09:00»: a moment with its preposition. */
export const atPhrase = (at: number, now: number): string => {
  const when = moment(at, now);
  return t(when.dated ? 'next.limit.on' : 'next.limit.at', { when: when.text });
};

/** «2 ч 14 мин», «14 мин», «1 д 3 ч»: what is left, rounded up to the minute (never «0 мин»). */
export const leftText = (ms: number): string => {
  const minutes = Math.max(1, Math.ceil(ms / MINUTE));
  if (minutes >= DAY / MINUTE) {
    const hours = Math.floor(minutes / 60);
    return t('next.limit.left.days', { d: Math.floor(hours / 24), h: hours % 24 });
  }
  if (minutes >= 60) return t('next.time.durHours', { h: Math.floor(minutes / 60), m: minutes % 60 });
  return t('next.time.durMinutes', { m: minutes });
};

export const limitTitle = (hold: Pick<LimitHold, 'provider' | 'window'>): string =>
  t(hold.window === 'weekly' ? 'next.limit.titleWeekly' : 'next.limit.title', {
    provider: PROVIDER_NAME[hold.provider],
  });

/** The card's line: «Лимит Claude исчерпан · продолжу автоматически в 18:00 (через 2 ч 14 мин)». */
export const limitLine = (hold: LimitHold, now: number): string => {
  const title = limitTitle(hold);
  if (hold.continueAt !== null)
    return t('next.limit.line.auto', {
      title,
      at: atPhrase(hold.continueAt, now),
      left: leftText(hold.continueAt - now),
    });
  if (hold.resetsAt !== null)
    return t('next.limit.line.held', { title, when: moment(hold.resetsAt, now).text });
  return title;
};

/** The sidebar row's words: «Ждёт сброса лимита до 18:00», or without the time when none is known. */
export const limitWaitText = (hold: Pick<LimitHold, 'resetsAt'>, now: number): string =>
  hold.resetsAt === null
    ? t('next.chatStatus.limited')
    : t('next.chatStatus.limitedUntil', { when: moment(hold.resetsAt, now).text });

/** The field's hint while held: what a message written now waits for. */
export const limitPlaceholder = (hold: Pick<LimitHold, 'resetsAt'>, now: number): string =>
  hold.resetsAt === null
    ? t('next.composer.placeholderLimitedUnknown')
    : t('next.composer.placeholderLimited', { at: atPhrase(hold.resetsAt, now) });
