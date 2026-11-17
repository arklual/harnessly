// The limits in the model pill's popover, quiet: a subscription's name, then a line a window — «5 ч ▬▬── 89%
// 21:00», «неделя ▬▬▬─ 45% сб 14:00» — and one grey line only when the machine's forecast says the limit runs out
// before its reset («Хватит до ср 09:00»). Only the windows the plan has stand: a model's own week
// («Fable · неделя») follows the plan's week. Colour is only on a number that runs low.
import { useLayoutEffect, useRef, useState } from 'react';
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
  type OtherLimits,
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
export const runsOutLine = (windows: readonly Shown[], nowSec: number = Date.now() / 1000): string | null => {
  const short = windows
    .flatMap(({ kind, window }) => {
      const f = window.forecast;
      if (f?.status !== FORECAST.exhausts || f.exhaustAt === null) return [];
      if (f.exhaustAt >= window.resetsAt) return [];
      const at = Math.round(f.exhaustAt / ROUND_SEC[kind]) * ROUND_SEC[kind];
      if (at >= window.resetsAt) return [];
      // The day is named only when it is not today: «пн 18:00» read on a Monday looks later than a Saturday reset.
      const today = new Date(at * 1000).toDateString() === new Date(nowSec * 1000).toDateString();
      return [{ at, weekly: kind !== WINDOW.fiveHour && !today }];
    })
    .sort((a, b) => a.at - b.at)[0];
  return short ? t('next.limits.brief.lastsTo', { at: clockOf(short.at, short.weekly) }) : null;
};

/** A model's own limit is the chat's when the model's id names it («claude-fable-5-2» and «Fable»). */
export const ofModel = (slot: ISlot, model: string | null): boolean =>
  slot.model !== null && model !== null && model.toLowerCase().includes(slot.model.toLowerCase());

/** A limit is told only when it presses: this much or less is left. */
const PRESS_AT = 25;
/** Another subscription this low is lifted out of the quiet summary line. */
const WARM_AT = 10;

/** What the chosen model spends: the plan's windows and the model's own week; of them the one that is nearest to
 *  its end, and the other only when it presses too. A plan's month stays for those who have nothing else. */
export const shownSlots = (slots: readonly ISlot[], model: string | null): ISlot[] => {
  const mine = slots.filter((s) => s.model === null || ofModel(s, model));
  const plan = mine.filter((s) => s.kind !== WINDOW.month);
  const pool = plan.length > 0 ? plan : mine;
  const sorted = [...pool].sort((a, b) => a.left - b.left);
  return sorted.filter((s, i) => i === 0 || s.left <= PRESS_AT).slice(0, 2);
};

const leastOf = (limits: ProviderLimits, nowSec: number): number | null => {
  const left = slotsOf(limits, nowSec).map((s) => s.left);
  return left.length > 0 ? Math.min(...left) : null;
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
      <span className="hx-limb__reset">
        {t('next.limits.brief.reset', { at: resetClock(slot.kind, slot.window.resetsAt) })}
      </span>
    </div>
  );
};

const Row = ({
  name,
  limits,
  now,
  model = null,
}: {
  name: string;
  limits: ProviderLimits | null;
  now: number;
  model?: string | null;
}) => {
  const all = slotsOf(limits, now / 1000);
  const slots = shownSlots(all, model);
  const forecast = runsOutLine(
    all.filter((s) => s.model === null),
    now / 1000,
  );
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

const Others = ({ others, now, fallback }: { others: OtherLimits[]; now: number; fallback: string }) => {
  const [open, setOpen] = useState(false);
  const nameOf = (o: OtherLimits) => o.name ?? t(`next.agent.${o.provider ?? fallback}`);
  const rows = others.map((o) => ({ o, left: leastOf(o.limits, now / 1000) }));
  const warm = rows.filter((r) => r.left !== null && r.left <= WARM_AT);
  const quiet = rows.filter((r) => !warm.includes(r));
  const summary = quiet
    .filter((r) => r.left !== null)
    .map((r) => `${nameOf(r.o)} ${r.left}%`)
    .join(' · ');
  return (
    <>
      {warm.map(({ o }) => (
        <div key={o.key} className="hx-limb__warm">
          <Row name={nameOf(o)} limits={o.limits} now={now} />
        </div>
      ))}
      {quiet.length > 0 && (
        <div className="hx-limo" data-open={open}>
          <button type="button" className="hx-limo__sum" aria-expanded={open} onClick={() => setOpen(!open)}>
            <span className="hx-limo__text">{t('next.limits.brief.others', { list: summary })}</span>
          </button>
          <div className="hx-limo__body">
            <div className="hx-limo__in">
              {quiet.map(({ o }) => (
                <Row key={o.key} name={nameOf(o)} limits={o.limits} now={now} />
              ))}
            </div>
          </div>
        </div>
      )}
    </>
  );
};

/** The limits of the chat's model, and the machine's other subscriptions in one quiet line. */
const LimitsBrief = ({ view, model = null }: { view: LimitsView; model?: string | null }) => {
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
        <Row name={name} limits={view.limits} now={now} model={model} />
      ) : (
        <p className="hx-limb__fc">{view.limits?.error ?? t('next.limits.none', { provider: name })}</p>
      )}
      {(view.others ?? []).length > 0 && (
        <Others others={view.others ?? []} now={now} fallback={view.provider} />
      )}
    </div>
  );
};

export default LimitsBrief;
