// A terminal provider failure at the end of a chat. It never starts work: the one action opens the existing manual
// handoff picker, where the person chooses an available model/profile.
import type { TerminalFailure } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/limit.css';

interface ITerminalFailureCardProps {
  failure: TerminalFailure | null;
  onChooseTarget: (() => void) | null;
}

const TerminalFailureCard = ({ failure, onChooseTarget }: ITerminalFailureCardProps) => {
  if (!failure || !failure.terminal || !failure.needsTarget) return null;
  return (
    <div className="hx-terminalfailure" role="alert">
      <Icon name="circle-alert" size="sm" className="hx-terminalfailure__ic" />
      <span className="hx-terminalfailure__body">
        <strong className="hx-terminalfailure__title">{t('next.terminalFailure.quota.title')}</strong>
        <span className="hx-terminalfailure__text">{t('next.terminalFailure.quota.lead')}</span>
        {failure.detail && <span className="hx-terminalfailure__detail">{failure.detail}</span>}
      </span>
      <Button size="sm" disabled={onChooseTarget === null} onClick={onChooseTarget ?? undefined}>
        {t('next.terminalFailure.choose')}
      </Button>
    </div>
  );
};

export default TerminalFailureCard;
