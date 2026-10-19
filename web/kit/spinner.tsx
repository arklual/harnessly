import { cx } from './cx';

export interface SpinnerProps {
  size?: 'xs' | 's' | 'm' | 'l';
  className?: string;
  /** Announced to screen readers; omit for a decorative spinner. */
  label?: string;
}

/** An indeterminate activity ring that takes the current text colour. */
export function Spinner({ size = 'm', className, label }: SpinnerProps) {
  return (
    <span
      className={cx('k-spinner', className)}
      data-size={size}
      role={label ? 'status' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      <svg viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.2" strokeWidth="3" />
        <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
      </svg>
    </span>
  );
}

/** A placeholder block that pulses while content loads. */
export function Skeleton({
  width,
  height = '1em',
  className,
}: {
  width?: string;
  height?: string;
  className?: string;
}) {
  return <span className={cx('k-skeleton', className)} style={{ width, height }} aria-hidden />;
}
