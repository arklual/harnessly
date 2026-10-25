// What a person sees on a design they have no place in: who owns it and a way to ask. The runtime hides the project
// (it is not in their library), so the card is driven by the gateway's «role: null», not by a refusal.
import RequestForm from '@/next/features/designly/access/request-form';
import type { IProjectAccess } from '@/next/features/designly/access/api';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

interface INoAccessProps {
  project: string;
  access: IProjectAccess;
  onAsked: (next: IProjectAccess) => void;
  onBack: () => void;
}

const NoAccess = ({ project, access, onAsked, onBack }: INoAccessProps) => (
  <section className="hx-acc-card" aria-label={t('next.access.noAccess')}>
    <h2 className="hx-acc-card__title">{t('next.access.noAccess')}</h2>
    {access.ownerName && (
      <p className="hx-acc-card__lead">{t('next.access.owner', { name: access.ownerName })}</p>
    )}
    <RequestForm project={project} role={null} request={access.request} onAsked={onAsked} />
    <button type="button" className="hx-btn" data-variant="ghost" onClick={onBack}>
      {t('next.access.back')}
    </button>
  </section>
);

export default NoAccess;
