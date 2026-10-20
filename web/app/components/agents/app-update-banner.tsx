import { RefreshCw, X } from 'lucide-react';
import useAppUpdate from '@/hooks/use-app-update';
import { t } from '@/lib/i18n';

/** «A new version is out · Refresh» over the conversation once the server serves a newer build. */
const AppUpdateBanner = () => {
  const update = useAppUpdate();
  if (!update.available) return null;
  return (
    <div className="tw-update-banner" role="status">
      <RefreshCw aria-hidden="true" />
      <span>{t('agents.appUpdateBanner.newVersion')}</span>
      <button type="button" className="tw-update-banner-go" onClick={update.reload}>
        {t('agents.appUpdateBanner.refresh')}
      </button>
      <button
        type="button"
        className="tw-update-banner-close"
        aria-label={t('agents.appUpdateBanner.later')}
        title={t('agents.appUpdateBanner.later')}
        onClick={update.dismiss}
      >
        <X aria-hidden="true" />
      </button>
    </div>
  );
};

export default AppUpdateBanner;
