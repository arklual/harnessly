// What stands in the place of an app's editor when the app is not installed; and, above the editor, why a
// document opened for reading only (a migration did not fit, it is newer than the app, the person may only view it). Plain words, and the detail small.
import { Button } from '@/next/ui/button';
import { READ_ONLY_CODES, type TReadOnlyCode } from '@/next/features/app-catalog/api';
import { t } from '@/next/i18n';
import '@/next/styles/app-doc.css';

const isCode = (code: string | undefined): code is TReadOnlyCode =>
  READ_ONLY_CODES.some((known) => known === code);

export const NeedsApp = ({ app, onOpenApps }: { app: string; onOpenApps: () => void }) => (
  <div className="hx-appdoc__note" role="status">
    <h2 className="hx-appdoc__title">{t('next.appdoc.needsApp', { app })}</h2>
    <p className="hx-appdoc__text">{t('next.appdoc.needsAppText')}</p>
    <Button variant="ghost" onClick={onOpenApps}>
      {t('next.appdoc.toApps')}
    </Button>
  </div>
);

/** One quiet line above the editor of a document that may only be read: why, in plain words, the detail small. */
export const ReadOnly = ({ code, reason }: { code: string | undefined; reason: string | undefined }) => (
  <p className="hx-appdoc__strip" role="status">
    <span>{t('next.appdoc.readOnly')}</span>
    {isCode(code) && <span className="hx-appdoc__why">{t(`next.appdoc.code.${code}`)}</span>}
    {reason && <span className="hx-appdoc__detail">{t('next.appdoc.details', { reason })}</span>}
  </p>
);
