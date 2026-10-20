import type { ButtonHTMLAttributes, ReactNode, Ref } from 'react';
import { cx } from './dom';
import { TwSpinner } from './icons';

export const BUTTON_VARIANT = {
  primary: 'primary',
  secondary: 'secondary',
  ghost: 'ghost',
  danger: 'danger',
} as const;

export const BUTTON_SIZE = {
  sm: 'sm',
  md: 'md',
} as const;

export interface ITwButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'type'> {
  variant?: (typeof BUTTON_VARIANT)[keyof typeof BUTTON_VARIANT];
  size?: (typeof BUTTON_SIZE)[keyof typeof BUTTON_SIZE];
  icon?: ReactNode;
  /** A pessimistic action is in flight: spinner, clicks ignored, focus kept (aria-disabled, not disabled). */
  pending?: boolean;
  type?: 'button' | 'submit';
  ref?: Ref<HTMLButtonElement>;
}

export const TwButton = ({
  variant = BUTTON_VARIANT.secondary,
  size = BUTTON_SIZE.md,
  icon,
  pending = false,
  type = 'button',
  className,
  children,
  onClick,
  ...rest
}: ITwButtonProps) => (
  <button
    {...rest}
    type={type}
    aria-disabled={pending || rest['aria-disabled'] || undefined}
    aria-busy={pending || undefined}
    data-variant={variant}
    data-size={size}
    className={cx('tw-ui-button', className)}
    onClick={(event) => {
      if (pending) {
        event.preventDefault();
        return;
      }
      onClick?.(event);
    }}
  >
    {pending ? <TwSpinner /> : icon}
    {children && <span className="tw-ui-button-label">{children}</span>}
  </button>
);
