// What is left of the agent's subscription, by the model pill (no design for it: built from the design's pieces): a
// chip with the five-hour and the weekly window, «5ч 94% · нед 81%», and on hover (a tap on touch) a card with both windows, their
// resets and the machine's forecast (the design's .hx-gauge of the Economics tool). Only what the machine reports is
// drawn; an agent that reports nothing gets a chip with a dash and says so in the card. The card stays while the
// pointer travels to it (ui/use-hover-intent.ts) and a click pins it.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import LoadFailed, { LOAD_FAILED_SIZE } from '@/next/ui/load-failed';
import {
  clockOf,
  describeForecast,
  leftAtReset,
  leftOf,
  levelOf,
  span,
  WINDOW,
  windowName,
  windowShort,
  windowsOf,
  type LimitsView,
  type LimitWindow,
  type WindowKind,
} from '@/next/features/limits/model';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { useFitInViewport, useHoverIntent } from '@/next/ui/use-hover-intent';
import '@/next/styles/limits.css';

const EXIT_MS = 220;
/** The card's times are minutes: they move once in this long. */
const TICK_MS = 30_000;

const useNow = (): number => {
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
  const weekly = kind === WINDOW.sevenDay;
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
          at: clockOf(window.resetsAt, weekly),
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

export interface ILimitsChipProps {
  view: LimitsView;
}

const LimitsChip = ({ view }: ILimitsChipProps) => {
  const { open, pinned, setTrigger, setFloating, getFloating, triggerProps, floatingProps } =
    useHoverIntent();
  const { mounted, state } = usePresentFlag(open, EXIT_MS);
  const now = useNow();
  useFitInViewport(getFloating, mounted);

  const provider = t(`next.agent.${view.provider}`);
  const limits = view.state === 'ready' ? view.limits : null;
  const windows = windowsOf(limits);
  const lefts = windows.map((w) => leftOf(w.window));
  const least = windows.length > 1 ? lefts.indexOf(Math.min(...lefts)) : -1;
  const summary =
    windows
      .map((w) =>
        t('next.limits.summaryItem', {
          window: windowShort(w.kind),
          left: leftOf(w.window),
          span: span(w.window.resetsAt - now / 1000),
        }),
      )
      .join('; ') || t('next.limits.summaryNone');

  let note: string | null = null;
  if (view.state === 'failed' && !view.onRetry) note = t('next.limits.failed');
  else if (view.state === 'ready' && windows.length === 0) note = t('next.limits.none', { provider });

  const foot = [
    limits?.plan ?? null,
    limits?.updatedAt ? t('next.limits.updated', { at: clockOf(limits.updatedAt / 1000, false) }) : null,
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <div className="hx-lim">
      <button
        ref={setTrigger}
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
            key={w.kind}
            className="hx-lim__win"
            data-level={levelOf(lefts[i])}
            data-least={i === least || undefined}
          >
            <small>{windowShort(w.kind)}</small>
            {lefts[i]}%
          </span>
        ))}
        {windows.length === 0 && <span className="hx-lim__win">—</span>}
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
          <div className="hx-lim__title">{t('next.limits.title', { provider })}</div>
          {windows.map((w) => (
            <Gauge key={w.kind} kind={w.kind} window={w.window} provider={provider} now={now} />
          ))}
          {view.state === 'failed' && view.onRetry && (
            <LoadFailed
              size={LOAD_FAILED_SIZE.inline}
              title={t('next.load.limits')}
              reason={t('next.limits.failed')}
              onRetry={view.onRetry}
            />
          )}
          {note && <p className="hx-lim__note">{note}</p>}
          {limits?.error && <p className="hx-lim__note">{limits.error}</p>}
          {foot && <p className="hx-lim__foot">{foot}</p>}
        </div>
      )}
    </div>
  );
};

export default LimitsChip;
