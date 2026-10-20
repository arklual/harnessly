import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';
import { STATUS_TONE, type TStatusTone } from '@/lib/hl-status-tone';

interface IStatusDotProps {
  tone: TStatusTone;
  /** In the text flow instead of the corner of the icon it sits on. */
  inline?: boolean;
  className?: string;
}

/**
 * The state of a conversation as a 10 px badge on its icon: a pulsing accent dot while it works, amber while it waits for you,
 * green with a tick when it is done, red on an error, a muted grey one when it is not connected. The meaning is in the
 * tooltip and the accessible name of the host, so the badge itself is decoration.
 */
const StatusDot = ({ tone, inline = false, className }: IStatusDotProps) => (
  <span
    className={cn('tw-status-dot', inline && 'tw-status-dot-inline', className)}
    data-tone={tone}
    aria-hidden="true"
  >
    {tone === STATUS_TONE.done && <Check strokeWidth={3.4} />}
  </span>
);

export default StatusDot;
