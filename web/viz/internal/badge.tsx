// SPDX-License-Identifier: Apache-2.0
import type { ReactNode } from 'react';

import { classNames } from './cx';

export type VizBadgeTone = 'green' | 'amber' | 'rose' | 'blue' | 'teal' | 'purple' | 'neutral';

export interface VizBadgeProps {
  readonly tone: VizBadgeTone;
  readonly children: ReactNode;
  /** Dot before the label; default on. */
  readonly dot?: boolean;
  /** Upper-case label; default on. */
  readonly uppercase?: boolean;
}

/** Status pill: tone colours come from the accent tokens. */
export function VizBadge({ tone, children, dot = true, uppercase = true }: VizBadgeProps) {
  return (
    <span
      className={classNames(
        'viz-badge',
        `viz-badge--${tone}`,
        dot && 'viz-badge--dot',
        !uppercase && 'viz-badge--normal-case',
      )}
    >
      {children}
    </span>
  );
}
