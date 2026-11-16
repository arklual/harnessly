// The chat's limits right in the field, by the context ring: thin bars filled by what is left — the plan's
// windows it has now (the five hours when the plan has them, the week) and, when the chat's model has a limit of its
// own («Fable»), that one too. On hover the card of the context ring's kind: the least left as the
// big figure, then every window with what is left, its bar and its reset, the balance where the vendor has one, and the
// machine's forecast when the limit runs out before its reset. No figure is made up: no reading, the card says so.
import { memo, useLayoutEffect, useRef } from 'react';
import CtxCard from '@/next/features/composer/ctx-card';
import { ofModel, runsOutLine, slotsOf, type ISlot } from '@/next/features/limits/limits-brief';
import { useNow } from '@/next/features/limits/limits-chip';
import {
  LEVEL,
  WINDOW,
  clockOf,
  money,
  profileName,
  resetClock,
  span,
  windowName,
  type LimitsView,
} from '@/next/features/limits/model';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/limits.css';

const DAY_SEC = 24 * 3600;

/** «сброс через 2 ч 10 мин» within a day, «сброс в пн 10:00» further, «сброс 1 дек.» for a month. */
export const resetWords = (slot: ISlot, nowSec: number): string => {
  const left = slot.window.resetsAt - nowSec;
  if (left < DAY_SEC) return t('next.limits.card.resetIn', { span: span(left) });
  if (slot.kind === WINDOW.month)
    return t('next.limits.card.resetAt', { at: resetClock(slot.kind, slot.window.resetsAt) });
  return t('next.limits.pop.resetAt', { at: clockOf(slot.window.resetsAt, true) });
};

/** The window's name in the card: the plan's in full («5 часов», «Неделя»), a model's as the brief has it. */
const nameOf = (slot: ISlot): string => (slot.model === null ? windowName(slot.kind) : slot.label);

const Win = ({ slot, j, nowSec }: { slot: ISlot; j: number; nowSec: number }) => {
  const barRef = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    barRef.current?.querySelector('i')?.style.setProperty('--w', `${slot.left}%`);
  }, [slot.left]);
  return (
    <div className="hx-ctx__win" data-level={slot.level}>
      <div className="hx-ctx__row" data-j={j}>
        <span className="hx-ctx__label">{nameOf(slot)}</span>
        <span className="hx-ctx__val">{`${slot.left}%`}</span>
      </div>
      <div className="hx-ctx__bar" data-size="thin" ref={barRef}>
        <i data-k="msg" />
      </div>
      <div className="hx-ctx__used">{resetWords(slot, nowSec)}</div>
    </div>
  );
};

interface ILimitsMiniProps {
  view: LimitsView;
  /** The chat's model id; null when unknown. */
  model?: string | null;
}

const LimitsMini = memo(({ view, model = null }: ILimitsMiniProps) => {
  const now = useNow();
  const nowSec = now / 1000;
  const ref = useRef<HTMLSpanElement>(null);
  const limits = view.state === 'ready' ? view.limits : null;
  const slots = slotsOf(limits, nowSec).filter((s) => s.model === null || ofModel(s, model));
  const plan = slots.filter((s) => s.kind !== WINDOW.month);
  const bars = plan.length > 0 ? plan : slots;
  const fills = bars.map((s) => s.left).join(',');

  useLayoutEffect(() => {
    const els = ref.current?.querySelectorAll<HTMLElement>('i');
    fills.split(',').forEach((f, i) => els?.[i]?.style.setProperty('--fill', `${f || 0}%`));
  }, [fills]);

  const balance = limits?.balance ? money(limits.balance) : null;
  const failed = view.state === 'failed';
  // Nothing to say and nothing that went wrong: no meter at all, as before.
  if (slots.length === 0 && !balance && !failed && !limits?.error) return null;

  const provider = view.profile ? profileName(view.profile) : t(`next.agent.${view.provider}`);
  const least = slots.reduce<ISlot | null>((a, s) => (a === null || s.left < a.left ? s : a), null);
  const words = slots.map((s) => `${s.label} ${s.left}%`).join(' · ');
  const label = words
    ? t('next.limits.mini.tip', { provider, windows: words })
    : t('next.limits.title', { provider });
  const forecast = runsOutLine(
    slots.filter((s) => s.model === null),
    nowSec,
  );
  const updated = limits?.updatedAt
    ? t('next.limits.updated', { at: clockOf(limits.updatedAt / 1000, false) })
    : null;

  let none: string | null = null;
  if (failed) none = t('next.limits.failed');
  else if (slots.length === 0 && !balance) none = limits?.error ?? t('next.limits.none', { provider });

  let big: string | null = null;
  let sub: string | null = none;
  if (least) {
    big = `${least.left}%`;
    sub = t('next.limits.pop.least', { window: least.label });
  } else if (balance) {
    big = balance;
    sub = t('next.limits.balance');
  }

  return (
    <CtxCard
      label={label}
      title={t('next.limits.title', { provider })}
      tone={least && least.level !== LEVEL.ok ? 'warn' : undefined}
      chipClassName="hx-ctx__chip hx-limmini"
      chipRef={(el) => {
        ref.current = el;
      }}
      chip={
        bars.length > 0 ? bars.map((s) => <i key={s.key} data-level={s.level} />) : <i data-none="true" />
      }
    >
      <div className="hx-ctx__head">
        <span className="hx-ctx__title">{t('next.limits.title', { provider })}</span>
        {big && <span className="hx-ctx__pct">{big}</span>}
      </div>
      {sub && <div className="hx-ctx__used">{sub}</div>}
      {slots.length > 0 && (
        <div className="hx-ctx__wins">
          {slots.map((s, j) => (
            <Win key={s.key} slot={s} j={j} nowSec={nowSec} />
          ))}
        </div>
      )}
      {least && balance && (
        <div className="hx-ctx__rows">
          <div className="hx-ctx__row" data-j={slots.length}>
            <span className="hx-ctx__label">{t('next.limits.balance')}</span>
            <span className="hx-ctx__val">{balance}</span>
          </div>
        </div>
      )}
      {(forecast || updated) && (
        <div className="hx-ctx__foot" data-tone={forecast && least && least.left <= 10 ? 'low' : undefined}>
          <Icon name={forecast ? 'gauge' : 'clock'} size="sm" />
          <span>{forecast ?? updated}</span>
        </div>
      )}
    </CtxCard>
  );
});

export default LimitsMini;
