// What an installed app waits for the person to agree to, shown where they see the app: its card's details
// in «Приложения» and its own page. The real waiting access, as the runtime reads it where the consent is checked —
// not the manifest's rights as they were at install. Only a showing: the word itself is the install screen's.
import AccessList from '@/next/features/app-consent/access-list';
import type { IAccess, IInstalledApp } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';

type TWaiting = Pick<IInstalledApp, 'version' | 'access' | 'pending' | 'consent'>;

/** What the app waits for: everything a first install reaches, or only what a waiting update adds. */
export const waitingAccessOf = (app: TWaiting): readonly IAccess[] =>
  app.pending ? app.pending.access : (app.access ?? []);

export interface IConsentBlockProps {
  app: IInstalledApp;
  className?: string;
}

const ConsentBlock = ({ app, className }: IConsentBlockProps) => {
  const update = app.pending !== undefined;
  return (
    <section
      className={['hx-consentblock', className].filter(Boolean).join(' ')}
      aria-label={t('next.consent.waitsTitle')}
    >
      <h3 className="hx-consentblock__h">
        {update
          ? t('next.consent.updateWaits', { version: app.pending?.version ?? app.version })
          : t('next.consent.waitsTitle')}
      </h3>
      <AccessList access={waitingAccessOf(app)} fresh={update} />
      <p className="hx-consent__note">{t('next.consent.whereHint')}</p>
    </section>
  );
};

export default ConsentBlock;
