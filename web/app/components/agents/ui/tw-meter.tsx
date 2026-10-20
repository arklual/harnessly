import { cx } from './dom';

export const METER_TONE = {
  accent: 'accent',
  success: 'success',
  warning: 'warning',
  danger: 'danger',
  /** Accent below warnAt, warning below dangerAt, danger above. For limits and budgets. */
  auto: 'auto',
} as const;

type TMeterTone = (typeof METER_TONE)[keyof typeof METER_TONE];
type TMeterFixedTone = Exclude<TMeterTone, typeof METER_TONE.auto>;

export const METER_SIZE = {
  sm: 'sm',
  md: 'md',
} as const;

export interface ITwMeterProps {
  value: number;
  max: number;
  min?: number;
  /** Accessible name, e.g. "5-hour limit". */
  label: string;
  /** Spoken value, e.g. "3 of 7 done". Defaults to a rounded percentage. */
  valueText?: string;
  tone?: TMeterTone;
  warnAt?: number;
  dangerAt?: number;
  /** A forecast drawn as a lighter segment beyond the current value. */
  projected?: number;
  size?: (typeof METER_SIZE)[keyof typeof METER_SIZE];
  className?: string;
}

const clampRatio = (value: number, min: number, max: number) => {
  if (!(max > min) || !Number.isFinite(value)) return 0;
  return Math.min(1, Math.max(0, (value - min) / (max - min)));
};

/** Resolves an "auto" tone from the filled share. */
export const meterTone = (ratio: number, warnAt: number, dangerAt: number): TMeterFixedTone => {
  if (ratio >= dangerAt) return METER_TONE.danger;
  if (ratio >= warnAt) return METER_TONE.warning;
  return METER_TONE.accent;
};

const percent = (ratio: number) => `${Math.round(ratio * 1000) / 10}%`;

/**
 * Horizontal bar (role="meter"). Geometry lives in SVG width attributes, so no inline style is needed;
 * colours come from --t-* tokens through data-tone.
 */
export const TwMeter = ({
  value,
  max,
  min = 0,
  label,
  valueText,
  tone = METER_TONE.accent,
  warnAt = 0.7,
  dangerAt = 0.9,
  projected,
  size = METER_SIZE.sm,
  className,
}: ITwMeterProps) => {
  const ratio = clampRatio(value, min, max);
  const projectedRatio = projected === undefined ? null : Math.max(ratio, clampRatio(projected, min, max));
  const resolvedTone = tone === METER_TONE.auto ? meterTone(projectedRatio ?? ratio, warnAt, dangerAt) : tone;

  return (
    <svg
      role="meter"
      aria-label={label}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={Math.min(Math.max(value, min), max)}
      aria-valuetext={valueText ?? `${Math.round(ratio * 100)}%`}
      data-tone={resolvedTone}
      data-size={size}
      className={cx('tw-ui-meter', className)}
      preserveAspectRatio="none"
      focusable="false"
    >
      <rect className="tw-ui-meter-track" x="0" y="0" width="100%" height="100%" />
      {projectedRatio !== null && projectedRatio > ratio && (
        <rect className="tw-ui-meter-projected" x="0" y="0" width={percent(projectedRatio)} height="100%" />
      )}
      <rect className="tw-ui-meter-fill" x="0" y="0" width={percent(ratio)} height="100%" />
    </svg>
  );
};
