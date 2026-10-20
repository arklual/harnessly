import { useMemo, useState, type PointerEvent } from 'react';
import { TriangleAlert } from 'lucide-react';
import { METER_TONE, TwMeter, cx } from '@/components/agents/ui';
import {
  FORECAST_METHOD,
  FORECAST_TONE,
  describeForecast,
  describePace,
  formatDuration,
  type IForecast,
  type TFormatClock,
  type TForecastWindowKind,
} from '@/lib/hl-limits-forecast';
import type { ILimitWindow } from '@/types/server/hl-machines';
import { locale, t } from '@/lib/i18n';

/** Number and weekday formats of the page's language. */
const LOCALE_TAG = locale() === 'ru' ? 'ru-RU' : 'en-US';

/** Clock in the viewer's time zone: «16:50», with a day «Sat 13:30». */
export const useClock = (): TFormatClock =>
  useMemo(() => {
    const time = new Intl.DateTimeFormat(LOCALE_TAG, {
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23',
    });
    const day = new Intl.DateTimeFormat(LOCALE_TAG, { weekday: 'short' });
    return (epochSec: number, withDay: boolean) => {
      const date = new Date(epochSec * 1000);
      return withDay ? `${day.format(date)} ${time.format(date)}` : time.format(date);
    };
  }, []);

const NUMBER = new Intl.NumberFormat(LOCALE_TAG);

/** «1 842 311». */
export const formatExactTokens = (value: number): string => NUMBER.format(value);

/** «1.8M», «412K», «950». */
export const formatShortTokens = (value: number): string => {
  if (value >= 1_000_000)
    return t('agents.limitForecast.millions', {
      value: new Intl.NumberFormat(LOCALE_TAG, { maximumFractionDigits: 1 }).format(value / 1_000_000),
    });
  if (value >= 1_000) return t('agents.limitForecast.thousands', { value: Math.round(value / 1_000) });
  return String(value);
};

/** «≈ $4,20». */
export const formatUsd = (value: number): string =>
  `$${new Intl.NumberFormat(LOCALE_TAG, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(value)}`;

/** «42 min», «3 h 10 min». */
export const formatMs = (ms: number): string => formatDuration(ms / 1000);

/** «40 s ago», «3 min ago». */
export const formatAgo = (epochSec: number, now = Date.now() / 1000): string => {
  const seconds = Math.max(0, Math.round(now - epochSec));
  if (seconds < 60) return t('agents.limitForecast.secondsAgo', { seconds });
  return t('agents.limitForecast.ago', { duration: formatDuration(seconds) });
};

const forecastBasis = (forecast: IForecast): string =>
  forecast.method === FORECAST_METHOD.profile
    ? t('agents.limitForecast.basisProfile', { days: forecast.historyDays ?? 0 })
    : t('agents.limitForecast.basisPace');

const CURVE_WIDTH = 240;
const CURVE_HEIGHT = 40;

interface IForecastCurveProps {
  forecast: IForecast;
  kind: TForecastWindowKind;
}

/** Expected use from now to the reset: nights and weekends show up as flat stretches, midnights as faint ticks. */
export const ForecastCurve = ({ forecast, kind }: IForecastCurveProps) => {
  const clock = useClock();
  const [hover, setHover] = useState<[number, number] | null>(null);
  const path = forecast.path;
  const end = forecast.resetsAt;
  if (!path || path.length < 2 || end === null) return null;
  const start = path[0][0];
  const span = Math.max(1, end - start);
  const x = (t: number) => ((t - start) / span) * CURVE_WIDTH;
  const y = (u: number) => CURVE_HEIGHT - (Math.min(100, u) / 100) * CURVE_HEIGHT;
  const line = path.map(([t, u], i) => `${i ? 'L' : 'M'}${x(t).toFixed(1)},${y(u).toFixed(1)}`).join('');
  const last = path[path.length - 1];
  const area = `${line}L${x(last[0]).toFixed(1)},${CURVE_HEIGHT}L0,${CURVE_HEIGHT}Z`;
  const weekly = kind === 'seven_day';
  const at = (t: number) => clock(t, weekly);
  const midnights: number[] = [];
  if (weekly) {
    const day = new Date(start * 1000);
    for (day.setHours(24, 0, 0, 0); day.getTime() / 1000 < end; day.setDate(day.getDate() + 1))
      midnights.push(day.getTime() / 1000);
  }
  const onMove = (event: PointerEvent<HTMLDivElement>) => {
    const box = event.currentTarget.getBoundingClientRect();
    const t = start + ((event.clientX - box.left) / box.width) * span;
    const point = path.reduce(
      (best, item) => (Math.abs(item[0] - t) < Math.abs(best[0] - t) ? item : best),
      path[0],
    );
    setHover(point);
  };
  const summary =
    forecast.exhaustAt === null
      ? t('agents.limitForecast.summaryAtReset', {
          now: Math.round(path[0][1]),
          reset: at(end),
          used: Math.round(last[1]),
        })
      : t('agents.limitForecast.summaryExhaust', { now: Math.round(path[0][1]), at: at(last[0]) });
  return (
    <div className="tw-forecast-curve" onPointerMove={onMove} onPointerLeave={() => setHover(null)}>
      <svg
        viewBox={`0 0 ${CURVE_WIDTH} ${CURVE_HEIGHT}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={summary}
      >
        {midnights.map((t) => (
          <line
            key={t}
            className="tw-forecast-curve-day"
            x1={x(t)}
            x2={x(t)}
            y1="0"
            y2={CURVE_HEIGHT}
            vectorEffect="non-scaling-stroke"
          />
        ))}
        <line
          className="tw-forecast-curve-limit"
          x1="0"
          x2={CURVE_WIDTH}
          y1="0.5"
          y2="0.5"
          vectorEffect="non-scaling-stroke"
        />
        <path className="tw-forecast-curve-area" d={area} />
        <path className="tw-forecast-curve-line" d={line} vectorEffect="non-scaling-stroke" />
        {hover && (
          <line
            className="tw-forecast-curve-cursor"
            x1={x(hover[0])}
            x2={x(hover[0])}
            y1="0"
            y2={CURVE_HEIGHT}
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      <div className="tw-forecast-curve-axis">
        <span>
          {hover
            ? `${at(hover[0])} · ≈ ${Math.round(hover[1])}%`
            : t('agents.limitForecast.now', { used: Math.round(path[0][1]) })}
        </span>
        <span>{t('agents.limitForecast.reset', { at: at(end) })}</span>
      </div>
    </div>
  );
};

interface ILimitForecastProps {
  window: ILimitWindow;
  kind: TForecastWindowKind;
  providerName: string;
  /** Also shows the codexbar average line. */
  withPace?: boolean;
  /** Draws the expected use until the reset under the text. */
  withCurve?: boolean;
  className?: string;
}

/** Forecast of one window and, under it, codexbar's own average: they look at different horizons, so both stay. */
export const LimitForecast = ({
  window,
  kind,
  providerName,
  withPace = true,
  withCurve = true,
  className,
}: ILimitForecastProps) => {
  const clock = useClock();
  if (!window.forecast) return null;
  const text = describeForecast(window.forecast, kind, providerName, clock);
  return (
    <div
      className={cx('tw-forecast', className)}
      data-tone={text.tone}
      title={forecastBasis(window.forecast)}
    >
      <p className="tw-forecast-line">
        {text.tone !== FORECAST_TONE.neutral && <TriangleAlert aria-hidden="true" />}
        <span>{text.text}</span>
      </p>
      {text.detail && <p className="tw-forecast-detail">{text.detail}</p>}
      {withCurve && <ForecastCurve forecast={window.forecast} kind={kind} />}
      {withPace && window.pace && <p className="tw-forecast-detail">{describePace(window.pace)}</p>}
    </div>
  );
};

interface ILimitWindowRowProps {
  label: string;
  window: ILimitWindow | null;
  kind: TForecastWindowKind;
  providerName: string;
  withForecast?: boolean;
}

/** «5 h ▓▓▓░░ 46% · resets at 01:20» and the forecast under it. */
export const LimitWindowRow = ({
  label,
  window,
  kind,
  providerName,
  withForecast = true,
}: ILimitWindowRowProps) => {
  const clock = useClock();
  if (!window) return null;
  const used = Math.round(window.usedPct);
  const reset = clock(window.resetsAt, kind === 'seven_day');
  return (
    <div className="tw-econ-limit">
      <div className="tw-econ-limit-row">
        <span className="tw-econ-limit-label">{label}</span>
        <TwMeter
          value={Math.min(100, window.usedPct)}
          max={100}
          label={t('agents.limitForecast.meterLabel', { provider: providerName, window: label })}
          valueText={t('agents.limitForecast.meterValue', { used, reset })}
          tone={METER_TONE.auto}
          warnAt={0.6}
          dangerAt={0.85}
          projected={window.forecast?.usedAtReset ?? undefined}
          className="tw-econ-limit-meter"
        />
        <span className="tw-econ-limit-value">{used}%</span>
        <span className="tw-econ-limit-reset">{t('agents.limitForecast.reset', { at: reset })}</span>
      </div>
      {withForecast && <LimitForecast window={window} kind={kind} providerName={providerName} />}
    </div>
  );
};
