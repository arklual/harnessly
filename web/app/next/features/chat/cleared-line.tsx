// The quiet line over a message for which a screen the person left open in the agent's terminal was closed first —
// their /login, the model picker, the effort slider: the message could not go in while it was up.
import type { TerminalAside } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/limit.css';

const ClearedLine = ({ what }: { what: TerminalAside }) => (
  <div className="hx-autoline" role="status">
    <Icon name="square-terminal" size="sm" className="hx-autoline__ic" />
    <span className="hx-autoline__text">{t(`next.cleared.${what}`)}</span>
  </div>
);

export default ClearedLine;
