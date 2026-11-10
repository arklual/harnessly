// The switches of an app on its page: the environment's app (a pinned one cannot be turned off, and says so) and an app
// the person installed (one click turns it off everywhere at once). Each waits for the machine and says why it failed.
import { useState } from 'react';
import Switch from '@/next/ui/switch';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';

/** The switch of the environment's app: a pinned one cannot be turned off, and says so. */
export const EnvSwitch = ({
  app,
  onMute,
}: {
  app: IInstalledApp;
  onMute: (app: IInstalledApp, mute: boolean) => Promise<void>;
}) => {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  if (app.locked) return <p className="hx-apppage__none">{t('next.appslist.envLocked')}</p>;
  const toggle = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await onMute(app, !app.muted);
    } catch (err) {
      setFailure(err instanceof Error ? err.message : t('next.appslist.envFailed'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <label className="hx-apppage__switch">
        <span>{t('next.appslist.enabled')}</span>
        <Switch
          label={t('next.appslist.enabled')}
          checked={!app.muted}
          disabled={busy}
          onChange={() => void toggle()}
        />
      </label>
      {failure && (
        <p className="hx-appteam__fail" role="alert">
          {failure}
        </p>
      )}
    </>
  );
};

/** The switch of an app the person installed (HRN-849): one click turns it off everywhere at once. */
export const OwnSwitch = ({
  app,
  onSwitch,
}: {
  app: IInstalledApp;
  onSwitch: (app: IInstalledApp, on: boolean) => Promise<void>;
}) => {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const toggle = async () => {
    setBusy(true);
    setFailure(null);
    try {
      await onSwitch(app, app.off === true);
    } catch (err) {
      setFailure(err instanceof Error ? err.message : t('next.appslist.envFailed'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <label className="hx-apppage__switch" title={t('next.consent.offHint')}>
        <span>{t('next.consent.enabled')}</span>
        <Switch
          label={t('next.consent.enabled')}
          checked={app.off !== true}
          disabled={busy}
          onChange={() => void toggle()}
        />
      </label>
      {failure && (
        <p className="hx-appteam__fail" role="alert">
          {failure}
        </p>
      )}
    </>
  );
};
