// What a person with a place in a project sees when their machine's library still has no such project after the
// waits: the project lives where their library does not reach (the owner's machine is off, or it is another library).
// It says so, who to ask, and offers to look again or to go back — never an empty screen.
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

interface IProjectAwayProps {
  /** The owner's name from the gateway; empty when it is not known. */
  ownerName: string;
  onRetry: () => void;
  onBack: () => void;
}

const ProjectAway = ({ ownerName, onRetry, onBack }: IProjectAwayProps) => (
  <section className="hx-acc-card" aria-label={t('next.access.away.title')} role="alert">
    <h2 className="hx-acc-card__title">{t('next.access.away.title')}</h2>
    <p className="hx-acc-card__lead">{t('next.access.away.lead')}</p>
    {ownerName && <p className="hx-acc-card__lead">{t('next.access.owner', { name: ownerName })}</p>}
    <div className="hx-acc-card__acts">
      <button type="button" className="hx-btn" data-variant="primary" onClick={onRetry}>
        {t('next.access.away.retry')}
      </button>
      <button type="button" className="hx-btn" data-variant="ghost" onClick={onBack}>
        {t('next.access.back')}
      </button>
    </div>
  </section>
);

export default ProjectAway;
