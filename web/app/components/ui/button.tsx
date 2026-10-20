// The plain button the dialogs use: solid for the main action, outline for the rest.
import type { ButtonHTMLAttributes } from 'react';
import { cn } from '@/lib/utils';

export interface IButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'default' | 'outline' | 'ghost' | 'destructive' | 'secondary';
  size?: 'default' | 'sm' | 'lg' | 'icon' | 'icon-sm' | 'icon-xs';
}

export function Button({
  variant = 'default',
  size = 'default',
  className,
  type = 'button',
  ...rest
}: IButtonProps) {
  return (
    <button
      type={type}
      className={cn('hl-btn', className)}
      data-variant={variant}
      data-size={size}
      {...rest}
    />
  );
}
