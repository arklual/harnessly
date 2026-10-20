import type { ButtonHTMLAttributes, HTMLAttributes, ReactNode, Ref } from 'react';
import { cx } from './dom';
import { TwChevronDownIcon, TwSpinner } from './icons';

export const CHIP_TONE = {
  neutral: 'neutral',
  accent: 'accent',
  success: 'success',
  warning: 'warning',
  danger: 'danger',
} as const;

type TChipTone = (typeof CHIP_TONE)[keyof typeof CHIP_TONE];

export interface ITwChipProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'type'> {
  icon?: ReactNode;
  tone?: TChipTone;
  /** Shows a chevron: the chip opens a popover or a menu. */
  expandable?: boolean;
  /** A pessimistic save is in flight: spinner instead of the icon, clicks ignored, focus kept. */
  pending?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

/**
 * Interactive pill: "Opus · High ▾", a project crumb, a header indicator. Labels truncate with an
 * ellipsis in narrow containers; pass aria-label when the visible text is abbreviated.
 */
export const TwChip = ({
  icon,
  tone = CHIP_TONE.neutral,
  expandable = false,
  pending = false,
  className,
  children,
  onClick,
  ...rest
}: ITwChipProps) => (
  <button
    {...rest}
    type="button"
    aria-disabled={pending || rest['aria-disabled'] || undefined}
    aria-busy={pending || rest['aria-busy'] || undefined}
    data-tone={tone}
    className={cx('tw-ui-chip', className)}
    onClick={(event) => {
      if (!pending) onClick?.(event);
    }}
  >
    {pending ? <TwSpinner /> : icon}
    {children && <span className="tw-ui-chip-label">{children}</span>}
    {expandable && (
      <span className="tw-ui-chip-chevron" aria-hidden="true">
        <TwChevronDownIcon />
      </span>
    )}
  </button>
);

export interface ITwBadgeProps extends HTMLAttributes<HTMLSpanElement> {
  tone?: TChipTone;
  icon?: ReactNode;
}

/** Static pill for statuses and sources ("built-in", "plugin · name"). */
export const TwBadge = ({ tone = CHIP_TONE.neutral, icon, className, children, ...rest }: ITwBadgeProps) => (
  <span {...rest} data-tone={tone} className={cx('tw-ui-badge', className)}>
    {icon}
    {children}
  </span>
);

/** Keyboard shortcut hint, e.g. ⌘1. */
export const TwKbd = ({ className, ...rest }: HTMLAttributes<HTMLElement>) => (
  <kbd {...rest} className={cx('tw-ui-kbd', className)} />
);
