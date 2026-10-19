import { type ButtonHTMLAttributes, type ReactNode, type Ref } from 'react';

import { cx } from './cx';
import { Spinner } from './spinner';

export type ButtonAppearance = 'primary' | 'accent' | 'outline' | 'secondary' | 'ghost' | 'danger';
export type ButtonSize = 'xs' | 's' | 'm' | 'l';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  appearance?: ButtonAppearance;
  size?: ButtonSize;
  /** Shows a spinner in place of the content (the width stays) and ignores clicks. */
  loading?: boolean;
  /** Icon before the label. */
  icon?: ReactNode;
  /** Icon after the label. */
  iconEnd?: ReactNode;
  block?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

export function Button({
  appearance = 'outline',
  size = 's',
  loading = false,
  icon,
  iconEnd,
  block,
  className,
  children,
  disabled,
  type = 'button',
  onClick,
  ...rest
}: ButtonProps) {
  return (
    <button
      {...rest}
      type={type}
      className={cx('k-btn', block && 'k-btn--block', className)}
      data-appearance={appearance}
      data-size={size}
      data-loading={loading || undefined}
      disabled={disabled}
      aria-busy={loading || undefined}
      onClick={loading ? (e) => e.preventDefault() : onClick}
    >
      <span className="k-btn__content">
        {icon}
        {children != null && <span className="k-btn__label">{children}</span>}
        {iconEnd}
      </span>
      {loading && (
        <span className="k-btn__spinner">
          <Spinner size="s" />
        </span>
      )}
    </button>
  );
}

export interface IconButtonProps extends Omit<ButtonProps, 'icon' | 'iconEnd' | 'children'> {
  /** Accessible name; also shown as the native tooltip unless title is given. */
  label: string;
  children: ReactNode;
}

/** A square button holding one icon. */
export function IconButton({
  label,
  title,
  className,
  appearance = 'ghost',
  children,
  ...rest
}: IconButtonProps) {
  return (
    <Button
      {...rest}
      appearance={appearance}
      aria-label={label}
      title={title ?? label}
      className={cx('k-btn--icon', className)}
      icon={children}
    />
  );
}
