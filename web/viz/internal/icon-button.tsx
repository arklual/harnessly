// SPDX-License-Identifier: Apache-2.0
import { type ButtonHTMLAttributes, type ReactNode, forwardRef } from 'react';

import { classNames } from './cx';

export interface VizIconButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  readonly size?: 'xs' | 's';
  readonly icon: ReactNode;
  readonly 'aria-label': string;
}

/** Flat square icon button of the chart controls (zoom, close). */
export const VizIconButton = forwardRef<HTMLButtonElement, VizIconButtonProps>(function VizIconButton(
  { size = 's', icon, className, type = 'button', ...rest },
  ref,
) {
  return (
    <button
      {...rest}
      ref={ref}
      type={type}
      className={classNames('viz-btn', 'viz-btn--icon', className)}
      data-size={size}
    >
      <span className="viz-btn__icon" aria-hidden="true">
        {icon}
      </span>
    </button>
  );
});
