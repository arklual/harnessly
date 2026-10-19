import type { HTMLAttributes, ReactNode } from 'react';

import { cx } from './cx';

export type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info';

export interface BadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: Tone;
  /** soft: a tinted fill; outline: a ring only. */
  look?: 'soft' | 'outline';
  icon?: ReactNode;
}

/** A short status or count label. */
export function Badge({ tone = 'neutral', look = 'soft', icon, className, children, ...rest }: BadgeProps) {
  return (
    <span {...rest} className={cx('k-badge', className)} data-tone={tone} data-look={look}>
      {icon}
      {children}
    </span>
  );
}

/** A round marker in a tone, e.g. an agent's state; it pulses while `live`. */
export function Dot({ tone = 'neutral', live, label }: { tone?: Tone; live?: boolean; label?: string }) {
  return (
    <span
      className="k-dot"
      data-tone={tone}
      data-live={live || undefined}
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    />
  );
}
