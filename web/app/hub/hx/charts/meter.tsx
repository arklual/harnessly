import type { CSSProperties } from 'react';

import { isHot, pctText } from './geometry';

/**
 * A load meter of a table row (`span.mt-meter`): the fill, the number, hot from 85%, dimmed while the machine is off.
 * What is not known (null) is an empty bar and a dash; the caller decides what a switched-off machine still shows.
 */
export const Meter = ({ pct, off }: { pct: number | null; off: boolean }) => (
  <span className="mt-meter" data-hot={isHot(pct) ? '1' : '0'} data-off={off ? '1' : '0'}>
    <i style={{ '--w': `${pct ?? 0}%` } as CSSProperties} />
    <b>{pctText(pct)}</b>
  </span>
);
