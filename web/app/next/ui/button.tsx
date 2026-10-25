// The kit's buttons, after components.css of the design: .hx-btn (text, pill) and .hx-round (an icon in a circle).
// A busy button keeps its place, turns its spinner on and ignores presses until its answer comes.
import type { ButtonHTMLAttributes, ReactNode, Ref } from 'react';
import Icon, { type TIconName } from '@/next/ui/icon';
import Spinner from '@/next/ui/spinner';
import Tooltip from '@/next/ui/tooltip';

type TNativeButton = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'type'>;

interface IButtonProps extends TNativeButton {
  variant?: 'default' | 'primary' | 'ghost' | 'danger';
  size?: 'sm' | 'md';
  busy?: boolean;
  icon?: TIconName;
  children: ReactNode;
  ref?: Ref<HTMLButtonElement>;
}

export const Button = ({
  variant = 'default',
  size = 'md',
  busy = false,
  icon,
  children,
  onClick,
  ref,
  ...rest
}: IButtonProps) => (
  <button
    {...rest}
    ref={ref}
    type="button"
    className={rest.className ? `hx-btn ${rest.className}` : 'hx-btn'}
    data-variant={variant === 'default' ? undefined : variant}
    data-size={size === 'sm' ? 'sm' : undefined}
    data-busy={busy || undefined}
    aria-busy={busy || undefined}
    onClick={busy ? undefined : onClick}
  >
    {icon && <Icon name={icon} size="sm" />}
    <span className="hx-btn__text">{children}</span>
    <Spinner />
  </button>
);

interface IRoundButtonProps extends TNativeButton {
  icon: TIconName;
  /** The name of the action: the button's label and its tooltip. */
  label: string;
  /** Keys that do the same, shown in the tooltip (e.g. ['⌃', '↵']). */
  keys?: string[];
  variant?: 'default' | 'accent' | 'ghost';
  size?: 'sm' | 'md';
  iconSize?: 'sm' | 'md';
  busy?: boolean;
  ref?: Ref<HTMLButtonElement>;
}

export const RoundButton = ({
  icon,
  label,
  keys,
  variant = 'default',
  size = 'md',
  iconSize = 'md',
  busy = false,
  onClick,
  ref,
  ...rest
}: IRoundButtonProps) => (
  <Tooltip label={label} keys={keys}>
    <button
      {...rest}
      ref={ref}
      type="button"
      className={rest.className ? `hx-round ${rest.className}` : 'hx-round'}
      data-variant={variant === 'default' ? undefined : variant}
      data-size={size === 'sm' ? 'sm' : undefined}
      data-busy={busy || undefined}
      aria-label={label}
      aria-busy={busy || undefined}
      onClick={busy ? undefined : onClick}
    >
      <Icon name={icon} size={iconSize} />
      <Spinner />
    </button>
  </Tooltip>
);
