import { cx } from './cx';
import { meterTone } from './meter-tone';

export interface MeterProps {
  /** 0..1 */
  value: number;
  label: string;
  /** Text after the label, e.g. "3.1 / 8 GB". */
  detail?: string;
  /** Fractions where the bar turns warning and danger. */
  warnAt?: number;
  dangerAt?: number;
  className?: string;
}

/** A labelled horizontal bar for a share of a limit. */
export function Meter({ value, label, detail, warnAt, dangerAt, className }: MeterProps) {
  const v = Number.isFinite(value) ? Math.min(Math.max(value, 0), 1) : 0;
  return (
    <div className={cx('k-meter', className)} data-tone={meterTone(v, warnAt, dangerAt)}>
      <div className="k-meter__text">
        <span className="k-meter__label">{label}</span>
        {detail != null && <span className="k-meter__detail">{detail}</span>}
      </div>
      <div
        className="k-meter__track"
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(v * 100)}
        aria-valuetext={detail}
      >
        <div className="k-meter__fill" style={{ width: `${v * 100}%` }} />
      </div>
    </div>
  );
}
