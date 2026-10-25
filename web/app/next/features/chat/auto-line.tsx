// The quiet line where the runtime went on by itself — after a usage limit reset, after a restart cut a turn short. It
// stands in place of the bubble of a message the runtime wrote, or right above the bubble of the person's own message
// that it sent. While the message is on its way the line says it is going on; once the agent has it, that it did.
import type { AutoContinueWhy } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/limit.css';

const ICON = { limit: 'clock', restart: 'rotate-ccw' } as const satisfies Record<AutoContinueWhy, string>;

export interface IAutoLineProps {
  why: AutoContinueWhy;
  /** The message has not become a turn yet. */
  pending: boolean;
}

const AutoLine = ({ why, pending }: IAutoLineProps) => (
  <div className="hx-autoline" role="status">
    <Icon name={ICON[why]} size="sm" className="hx-autoline__ic" />
    <span className="hx-autoline__text">{t(`next.auto.${why}.${pending ? 'pending' : 'done'}`)}</span>
  </div>
);

export default AutoLine;
