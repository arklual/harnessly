// The limits in the model pill's popover, quiet: a subscription's name, then a line a window — «5 ч ▬▬── 89%
// 21:00», «неделя ▬▬▬─ 45% сб 14:00» — and one grey line only when the machine's forecast says the limit runs out
// before its reset («Хватит до ср 09:00»). Only the windows the plan has stand: a model's own week
// («Fable · неделя») follows the plan's week. Colour is only on a number that runs low.
import { useLayoutEffect, useRef } from 'react';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import {
  FORECAST,
  WINDOW,
  clockOf,
  leftOf,
  levelOf,
  money,
  profileName,
  resetClock,
  type Level,
  type LimitsView,
  type LimitWindow,
  type ProviderLimits,
  type WindowKind,
} from '@/next/features/limits/model';
import { useNow } from '@/next/features/limits/limits-chip';
import { t } from '@/next/i18n';
import '@/next/styles/limits.css';

const BRIEF_KEY = {
  [WINDOW.fiveHour]: 'next.limits.brief.five',
  [WINDOW.sevenDay]: 'next.limits.brief.week',
  [WINDOW.month]: 'next.limits.brief.month',
} as const;

export const briefName = (kind: WindowKind): string => t(BRIEF_KEY[kind]);

/** A forecast is told to this precision: a guess to the minute is a lie. */
const ROUND_SEC: Record<WindowKind, number> = { five_hour: 5 * 60, seven_day: 3600, month: 3600 };

type Shown = { kind: WindowKind; window: LimitWindow };

/** One window as the card and the field's meter tell it: its key and name, what is left and its level. */
export interface ISlot {
  key: string;
  kind: WindowKind;
  label: string;
  window: LimitWindow;
  left: number;
  level: Level;
  /** The model a model's own limit is of («Fable»); null for the plan's windows. */
  model: string | null;
}

const slotOf = (
  key: string,
  kind: WindowKind,
  label: string,
  window: LimitWindow,
  model: string | null,
): ISlot => {
  const left = leftOf(window);
  return { key, kind, label, window, left, level: levelOf(left), model };
};

/** The windows the plan has now: one the machine does not report, or whose reading is of a window that has
 *  reset since, is not shown at all; a model's own week («Fable · неделя») follows the plan's week. */
export const slotsOf = (limits: ProviderLimits | null, nowSec: number): ISlot[] => {
  if (!limits) return [];
  const fresh = (w: LimitWindow | null): w is LimitWindow => w !== null && w.resetsAt > nowSec;
  const out: ISlot[] = [];
  if (fresh(limits.fiveHour))
    out.push(slotOf(WINDOW.fiveHour, WINDOW.fiveHour, briefName(WINDOW.fiveHour), limits.fiveHour, null));
  if (fresh(limits.sevenDay))
    out.push(slotOf(WINDOW.sevenDay, WINDOW.sevenDay, briefName(WINDOW.sevenDay), limits.sevenDay, null));
  for (const e of limits.extra ?? []) {
    if (fresh(e.window))
      out.push(
        slotOf(e.id, WINDOW.sevenDay, t('next.limits.brief.model', { model: e.title }), e.window, e.title),
      );
  }
  if (fresh(limits.month))
    out.push(slotOf(WINDOW.month, WINDOW.month, briefName(WINDOW.month), limits.month, null));
  return out;
};

/** When the limit runs out before its reset, as the line tells it; null when it lasts or the machine does not say. */
export const runsOutLine = (windows: readonly Shown[]): string | null => {
  const short = windows
    .flatMap(({ kind, window }) => {
      const f = window.forecast;
      if (f?.status !== FORECAST.exhausts || f.exhaustAt === null) return [];
      if (f.exhaustAt >= window.resetsAt) return [];
      const at = Math.round(f.exhaustAt / ROUND_SEC[kind]) * ROUND_SEC[kind];
      return [{ at, weekly: kind !== WINDOW.fiveHour }];
    })
    .sort((a, b) => a.at - b.at)[0];
  return short ? t('next.limits.brief.lastsTo', { at: clockOf(short.at, short.weekly) }) : null;
};

const Win = ({ slot, provider }: { slot: ISlot; provider: string }) => {
  const barRef = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => {
    barRef.current?.style.setProperty('--fill', `${slot.left}%`);
  }, [slot.left]);
  return (
    <div className="hx-limb__win" data-level={slot.level}>
      <span className="hx-limb__wname">{slot.label}</span>
      <span
        ref={barRef}
        className="hx-limb__bar"
        role="meter"
        aria-label={t('next.limits.brief.aria', { provider, window: slot.label, left: slot.left })}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={slot.left}
      />
      <span className="hx-limb__pct">{`${slot.left}%`}</span>
      <span className="hx-limb__reset">{resetClock(slot.kind, slot.window.resetsAt)}</span>
    </div>
  );
};

const Row = ({ name, limits, now }: { name: string; limits: ProviderLimits | null; now: number }) => {
  const slots = slotsOf(limits, now / 1000);
  const forecast = runsOutLine(slots.filter((s) => s.model === null));
  if (slots.length === 0 && !limits?.balance) return null;
  return (
    <div className="hx-limb">
      <div className="hx-limb__head">
        <b className="hx-limb__name">{name}</b>
        {slots.length === 0 && limits?.balance && (
          <span className="hx-limb__pct">{money(limits.balance)}</span>
        )}
      </div>
      {slots.map((slot) => (
        <Win key={slot.key} slot={slot} provider={name} />
      ))}
      {forecast && <p className="hx-limb__fc">{forecast}</p>}
    </div>
  );
};

/** The chat's limits and the machine's other subscriptions. */
const LimitsBrief = ({ view }: { view: LimitsView }) => {
  const now = useNow();
  const name = view.profile ? profileName(view.profile) : t(`next.agent.${view.provider}`);
  if (view.state === 'failed') {
    return view.onRetry ? (
      <LoadFailed
        size={LOAD_FAILED_SIZE.inline}
        title={t('next.load.limits')}
        reason={t('next.limits.failed')}
        onRetry={view.onRetry}
      />
    ) : (
      <p className="hx-limb__fc">{t('next.limits.failed')}</p>
    );
  }
  const own = view.limits !== null && (slotsOf(view.limits, now / 1000).length > 0 || view.limits.balance);
  return (
    <div className="hx-limbs">
      {own ? (
        <Row name={name} limits={view.limits} now={now} />
      ) : (
        <p className="hx-limb__fc">{view.limits?.error ?? t('next.limits.none', { provider: name })}</p>
      )}
      {(view.others ?? []).map((o) => (
        <Row
          key={o.key}
          name={o.name ?? t(`next.agent.${o.provider ?? view.provider}`)}
          limits={o.limits}
          now={now}
        />
      ))}
    </div>
  );
};

export default LimitsBrief;
