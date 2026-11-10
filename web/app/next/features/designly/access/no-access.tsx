// What a person sees on a design they have no place in: who owns it and a way to ask. The runtime hides the project
// (it is not in their library), so the card is driven by the gateway's «role: null», not by a refusal. Where the gateway
// answers the common access API (HRN-675) the request goes there, to the owner and those who run the access; otherwise
// to the older design API.
import RequestForm from '@/next/features/designly/access/request-form';
import { askAccess, type IProjectAccess } from '@/next/features/designly/access/api';
import { askFor, type IResourceView } from '@/next/features/designly/access/resource-api';
import { t } from '@/next/i18n';
import '@/next/styles/design-access.css';

interface INoAccessProps {
  project: string;
  access: IProjectAccess;
  onAsked: (next: IProjectAccess) => void;
  /** The common view of the project, when the gateway answers it and keeps the project: the request goes there. */
  view?: IResourceView | null;
  onView?: (next: IResourceView) => void;
  onBack: () => void;
}

const NoAccess = ({ project, access, onAsked, view = null, onView, onBack }: INoAccessProps) => {
  const common = view !== null && view.stored && onView !== undefined;
  const ownerName = common ? (view.owner?.name ?? '') : access.ownerName;
  return (
    <section className="hx-acc-card" aria-label={t('next.access.noAccess')}>
      <h2 className="hx-acc-card__title">{t('next.access.noAccess')}</h2>
      {ownerName && <p className="hx-acc-card__lead">{t('next.access.owner', { name: ownerName })}</p>}
      {common ? (
        <RequestForm
          mayComment
          status={view.myRequest?.status ?? null}
          onSend={async (level, message) =>
            onView(await askFor({ kind: 'design', id: project }, level, message))
          }
        />
      ) : (
        <RequestForm
          mayComment
          status={access.request?.status ?? null}
          onSend={async (level, message) => onAsked(await askAccess(project, level, message))}
        />
      )}
      <button type="button" className="hx-btn" data-variant="ghost" onClick={onBack}>
        {t('next.access.back')}
      </button>
    </section>
  );
};

export default NoAccess;
