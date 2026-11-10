// What a person with a place in a project sees when the project has not reached the center yet: its owner's
// machine made it and has not sent it to the hub's store, being off since. Projects in the store reach every machine
// whose person has a role, the owner's machine on or off; so this card is only for one still on its way. The page keeps
// looking on its own and opens the project when it arrives; the way back is always there — never an empty screen.
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

interface IProjectAwayProps {
  /** The owner's name from the gateway; empty when it is not known. */
  ownerName: string;
  onBack: () => void;
}

const ProjectAway = ({ ownerName, onBack }: IProjectAwayProps) => (
  <section className="hx-acc-card" aria-label={t('next.access.away.title')} role="status">
    <h2 className="hx-acc-card__title">{t('next.access.away.title')}</h2>
    <p className="hx-acc-card__lead">{t('next.access.away.lead')}</p>
    {ownerName && <p className="hx-acc-card__lead">{t('next.access.owner', { name: ownerName })}</p>}
    <div className="hx-acc-card__acts">
      <button type="button" className="hx-btn" data-variant="ghost" onClick={onBack}>
        {t('next.access.back')}
      </button>
    </div>
  </section>
);

export default ProjectAway;
