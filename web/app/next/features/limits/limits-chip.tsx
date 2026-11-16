// What is left of the agent's subscription, by the model pill (no design for it: built from the design's pieces): a
// chip with the five-hour and the weekly window, «5ч 94% · нед 81%», and on hover (a tap on touch) a card with both windows, their
// resets and the machine's forecast (the design's .hx-gauge of the Economics tool). Only what the machine reports is
// drawn; an agent that reports nothing gets a chip with a dash and says so in the card. The card stays while the
// pointer travels to it (ui/use-hover-intent.ts) and a click pins it. A machine that pays Claude by the API has no
// subscription windows: its chip tells what Claude cost today, «$4,27 сегодня», and its card the models it went to.
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import { usd } from '@/next/features/manage/format';
import {
  clockOf,
  dayStartOf,
  describeForecast,
  modelName,
  showsSpend,
  spentSince,
  type SpendView,
  leftAtReset,
  leftOf,
  levelOf,
  money,
  profileName,
  resetClock,
  span,
  windowName,
  windowShort,
  windowsOf,
  type LimitsView,
  type LimitWindow,
  type WindowKind,
} from '@/next/features/limits/model';
import LimitsLine from '@/next/features/limits/limits-line';
import CtxCard from '@/next/features/composer/ctx-card';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { useFitInViewport, useHoverIntent } from '@/next/ui/use-hover-intent';
import { useGlide } from '@/next/ui/use-glide';
import '@/next/styles/limits.css';

const EXIT_MS = 220;
/** The card's times are minutes: they move once in this long. */
const TICK_MS = 30_000;

export const useNow = (): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, []);
  return now;
};

interface IGaugeProps {
  kind: WindowKind;
  window: LimitWindow;
  provider: string;
  now: number;
}

/** One window: what is left, the meter (the tick is what the forecast leaves at the reset), the reset, the forecast. */
const Gauge = ({ kind, window, provider, now }: IGaugeProps) => {
  const meterRef = useRef<HTMLDivElement>(null);
  const left = leftOf(window);
  const atReset = leftAtReset(window);
  const forecast = window.forecast ? describeForecast(window.forecast, kind, provider) : null;
  const name = windowName(kind);

  useLayoutEffect(() => {
    meterRef.current?.style.setProperty('--fill', `${left}%`);
    if (atReset === null) meterRef.current?.style.removeProperty('--mark');
    else meterRef.current?.style.setProperty('--mark', `${atReset}%`);
  }, [left, atReset]);

  return (
    <div className="hx-gauge" data-level={levelOf(left)}>
      <div className="hx-gauge__top">
        <span>{name}</span>
        <span className="hx-spacer" />
        <b>{t('next.limits.left', { left })}</b>
      </div>
      <div
        ref={meterRef}
        className="hx-lim__meter"
        role="meter"
        aria-label={t('next.limits.meter', { provider, window: name, left })}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={left}
      >
        <i />
        {atReset !== null && <u title={t('next.limits.mark', { left: atReset })} />}
      </div>
      <span className="hx-gauge__sub">
        {t('next.limits.resets', {
          span: span(window.resetsAt - now / 1000),
          at: resetClock(kind, window.resetsAt),
        })}
      </span>
      {forecast && (
        <div className="hx-gauge__fc" data-tone={forecast.tone}>
          <span>{forecast.text}</span>
          {forecast.detail && <small>{forecast.detail}</small>}
        </div>
      )}
    </div>
  );
};

/** The colours of the spend's parts: the context card's own (the conversation's accent, then its other parts). */
const SPEND_KEYS = ['msg', 'mem', 'mcp', 'sys'] as const;

/** What Claude cost since the start of the person's day: the chip with the sum, and the card of the context ring's kind
 *  the sum, a bar of the models it went to, a row a model, and what this chat cost by Claude's own count. */
const SpendChip = ({ view, chatUsd }: { view: SpendView; chatUsd?: number }) => {
  const now = useNow();
  const barRef = useRef<HTMLDivElement>(null);
  const since = dayStartOf(now, view.dayStartHour);
  const today = useMemo(() => spentSince(view.spend, since), [view.spend, since]);
  const total = usd(today.usd);
  const from = clockOf(since, false);
  // The biggest models each on their own, the rest as one: the bar has as many parts as there are colours.
  const parts = useMemo(() => {
    const head = today.models
      .slice(0, SPEND_KEYS.length - 1)
      .map((m) => ({ label: modelName(m.model), usd: m.usd }));
    const rest = today.models.slice(SPEND_KEYS.length - 1);
    if (rest.length === 1) head.push({ label: modelName(rest[0].model), usd: rest[0].usd });
    else if (rest.length > 1)
      head.push({ label: t('next.limits.spend.rest'), usd: rest.reduce((sum, m) => sum + m.usd, 0) });
    return head.map((p, j) => ({ ...p, k: SPEND_KEYS[j] }));
  }, [today]);
  const shares = parts.map((p) => (today.usd > 0 ? (p.usd / today.usd) * 100 : 0)).join(',');

  useLayoutEffect(() => {
    const els = barRef.current?.querySelectorAll<HTMLElement>('i');
    shares.split(',').forEach((w, i) => els?.[i]?.style.setProperty('--w', `${w || 0}%`));
  }, [shares]);

  const sub = [
    t('next.limits.spend.since', { from }),
    chatUsd !== undefined && chatUsd > 0 ? t('next.limits.spend.chat', { sum: usd(chatUsd) }) : null,
  ]
    .filter(Boolean)
    .join(' · ');
  const foot = [
    t('next.limits.spend.api'),
    t('next.limits.updated', { at: clockOf(view.spend.asOf, false) }),
  ].join(' · ');

  return (
    <CtxCard
      label={t('next.limits.spend.aria', { sum: total, from })}
      title={t('next.limits.spend.title')}
      chipClassName="hx-lim__chip"
      wide
      chip={
        <>
          <Icon name="gauge" size="sm" />
          <span className="hx-lim__win">
            {total}
            <small>{t('next.limits.spend.today')}</small>
          </span>
        </>
      }
    >
      <div className="hx-ctx__head">
        <span className="hx-ctx__title">{t('next.limits.spend.title')}</span>
        <span className="hx-ctx__pct">{total}</span>
      </div>
      <div className="hx-ctx__used">{sub}</div>
      {parts.length > 0 && (
        <>
          <div className="hx-ctx__bar" data-fit="true" ref={barRef}>
            {parts.map((p) => (
              <i key={p.k} data-k={p.k} />
            ))}
          </div>
          <div className="hx-ctx__rows">
            {parts.map((p, j) => (
              <div key={p.k} className="hx-ctx__row" data-j={j}>
                <i className="hx-ctx__dot" data-k={p.k} />
                <span className="hx-ctx__label">{p.label}</span>
                <span className="hx-ctx__val">{usd(p.usd)}</span>
              </div>
            ))}
          </div>
        </>
      )}
      <div className="hx-ctx__foot">
        <Icon name="coins" size="sm" />
        <span>{foot}</span>
      </div>
    </CtxCard>
  );
};

export interface ILimitsChipProps {
  view: LimitsView;
  /** What this chat cost by the agent's own count, USD; told on a machine that pays by the API. */
  chatUsd?: number;
}

/** A machine that pays Claude by the API: no windows, the chip tells the day's spend. */
export const paysBySpend = (view: LimitsView): boolean =>
  view.state === 'ready' && view.spend !== null && showsSpend(view.limits, view.spend.spend);

// the chip sits in the field, which renders on each key; its view changes only with the limits.
const LimitsChip = memo(({ view, chatUsd }: ILimitsChipProps) =>
  view.state === 'ready' && view.spend && paysBySpend(view) ? (
    <SpendChip view={view.spend} chatUsd={chatUsd} />
  ) : (
    <WindowsChip view={view} />
  ),
);

/** The card of the chat's limits: both windows with their resets and forecast, the balance, the other subscriptions.
 *  The chip opens it, and so does the model pill's popover (on the pill the limit is its outline). */
export const LimitsCard = ({ view }: ILimitsChipProps) => {
  const now = useNow();
  const provider = providerOf(view);
  const limits = view.state === 'ready' ? view.limits : null;
  const windows = windowsOf(limits, now / 1000);
  const balance = limits?.balance ? money(limits.balance) : null;
  const others = view.state === 'ready' ? (view.others ?? []) : [];

  let note: string | null = null;
  if (view.state === 'failed' && !view.onRetry) note = t('next.limits.failed');
  else if (view.state === 'ready' && windows.length === 0 && !balance && !limits?.error)
    note = t('next.limits.none', { provider });

  const foot = [
    limits?.plan ?? null,
    limits?.estimate ? t('next.limits.estimate') : null,
    limits?.unofficial ? t('next.limits.unofficial') : null,
    limits?.updatedAt ? t('next.limits.updated', { at: clockOf(limits.updatedAt / 1000, false) }) : null,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <>
      <div className="hx-lim__title">{t('next.limits.title', { provider })}</div>
      {windows.map((w) => (
        <Gauge key={w.kind} kind={w.kind} window={w.window} provider={provider} now={now} />
      ))}
      {balance && (
        <div className="hx-lim__spend">
          <b>{balance}</b>
          <span>{t('next.limits.balance')}</span>
        </div>
      )}
      {view.state === 'failed' && view.onRetry && (
        <LoadFailed
          size={LOAD_FAILED_SIZE.inline}
          title={t('next.load.limits')}
          reason={t('next.limits.failed')}
          onRetry={view.onRetry}
        />
      )}
      {note && <p className="hx-lim__note">{note}</p>}
      {others.length > 0 && (
        <ul className="hx-lim__others" aria-label={t('next.limits.others')}>
          {others.map((other) => (
            <li key={other.key}>
              <span>{other.name ?? t(`next.agent.${other.provider ?? view.provider}`)}</span>
              <LimitsLine limits={other.limits} />
            </li>
          ))}
        </ul>
      )}
      {limits?.error && <p className="hx-lim__note">{limits.error}</p>}
      {foot && <p className="hx-lim__foot">{foot}</p>}
    </>
  );
};

const providerOf = (view: LimitsView): string =>
  view.profile ? profileName(view.profile) : t(`next.agent.${view.provider}`);

/** The chat's limits in one line, for the chip's and the pill's label: «5 ч: осталось 40%, …; неделя: …». */
export const limitsSummary = (view: LimitsView, nowSec: number): string => {
  const limits = view.state === 'ready' ? view.limits : null;
  const balance = limits?.balance ? money(limits.balance) : null;
  return (
    windowsOf(limits, nowSec)
      .map((w) =>
        t('next.limits.summaryItem', {
          window: windowShort(w.kind),
          left: leftOf(w.window),
          span: span(w.window.resetsAt - nowSec),
        }),
      )
      .join('; ') || (balance ? t('next.limits.balanceItem', { sum: balance }) : t('next.limits.summaryNone'))
  );
};

const WindowsChip = ({ view }: ILimitsChipProps) => {
  const { open, pinned, setTrigger, setFloating, getFloating, triggerProps, floatingProps } =
    useHoverIntent();
  const { mounted, state } = usePresentFlag(open, EXIT_MS);
  const now = useNow();
  useFitInViewport(getFloating, mounted);

  const provider = providerOf(view);
  const limits = view.state === 'ready' ? view.limits : null;
  const windows = windowsOf(limits, now / 1000);
  const balance = limits?.balance ? money(limits.balance) : null;
  const lefts = windows.map((w) => leftOf(w.window));
  const least = windows.length > 1 ? lefts.indexOf(Math.min(...lefts)) : -1;
  const summary = limitsSummary(view, now / 1000);

  // Another chat's limits (or a new reading) change the chip's numbers: they fade in (keyed) and the chip eases to its
  // new width instead of jumping.
  const [chip, setChip] = useState<HTMLButtonElement | null>(null);
  useGlide(chip, `${lefts.join(',')}|${balance ?? ''}`, 'width');
  const chipRef = useCallback(
    (node: HTMLButtonElement | null) => {
      setTrigger(node);
      setChip(node);
    },
    [setTrigger],
  );

  return (
    <div className="hx-lim">
      <button
        ref={chipRef}
        type="button"
        className="hx-lim__chip"
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-label={t('next.limits.aria', { provider, summary })}
        data-pinned={pinned || undefined}
        {...triggerProps}
      >
        <Icon name="gauge" size="sm" />
        {windows.map((w, i) => (
          <span
            key={`${w.kind}:${lefts[i]}`}
            className="hx-lim__win hx-lim__swap"
            data-level={levelOf(lefts[i])}
            data-least={i === least || undefined}
          >
            <small>{windowShort(w.kind)}</small>
            {lefts[i]}%
          </span>
        ))}
        {windows.length === 0 && balance && (
          <span className="hx-lim__win">
            {balance}
            <small>{t('next.limits.balanceShort')}</small>
          </span>
        )}
        {windows.length === 0 && !balance && <span className="hx-lim__win">—</span>}
      </button>
      {mounted && (
        <div
          ref={setFloating}
          className="hx-lim__pop hx-hover-fit"
          data-state={state}
          role="dialog"
          aria-label={t('next.limits.title', { provider })}
          {...floatingProps}
        >
          <LimitsCard view={view} />
        </div>
      )}
    </div>
  );
};

export default LimitsChip;
