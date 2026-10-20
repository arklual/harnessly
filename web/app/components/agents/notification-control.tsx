import { appHost } from '@/lib/site';
import { useState, useSyncExternalStore } from 'react';
import { ArrowLeft, Bell, BellOff, BellRing, Share, SquarePlus, Smartphone } from 'lucide-react';
import { toast } from 'sonner';
import { BUTTON_SIZE, BUTTON_VARIANT, TwButton } from '@/components/agents/ui';
import { installApp, useInstallStore } from '@/hooks/use-install-prompt';
import { disablePush, enablePush, sendTestPush, usePushStore, type TPushState } from '@/hooks/use-web-push';
import { t } from '@/lib/i18n';

export const PUSH_STATE_LABEL: Record<TPushState, string> = {
  unknown: t('agents.notificationControl.stateUnknown'),
  unsupported: t('agents.notificationControl.stateUnsupported'),
  'needs-install': t('agents.notificationControl.stateNeedsInstall'),
  denied: t('agents.notificationControl.stateDenied'),
  off: t('agents.notificationControl.stateOff'),
  on: t('agents.notificationControl.stateOn'),
};

const isAppleDevice = () =>
  typeof navigator !== 'undefined' && /iPad|iPhone|iPod|Macintosh/.test(navigator.userAgent);

const isTouchApple = () => typeof navigator !== 'undefined' && /iPad|iPhone|iPod/.test(navigator.userAgent);

/** Opened from the Home Screen or as an installed app window: nothing to install. */
const runsInstalled = () =>
  window.matchMedia('(display-mode: standalone)').matches ||
  (navigator as Navigator & { standalone?: boolean }).standalone === true;

const subscribeDisplayMode = (onChange: () => void) => {
  const query = window.matchMedia('(display-mode: standalone)');
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
};

const useRunsInstalled = (): boolean =>
  useSyncExternalStore(subscribeDisplayMode, runsInstalled, () => false);

const InstallSteps = () => (
  <ol className="tw-notify-steps">
    <li>
      <Share />
      <span>{t('agents.notificationControl.stepShare')}</span>
    </li>
    <li>
      <SquarePlus />
      <span>{t('agents.notificationControl.stepAddToHome')}</span>
    </li>
    <li>
      <Smartphone />
      <span>{t('agents.notificationControl.stepOpenApp')}</span>
    </li>
  </ol>
);

export const PushStateIcon = ({ state }: { state: TPushState }) => {
  if (state === 'on') return <BellRing />;
  if (state === 'denied' || state === 'unsupported') return <BellOff />;
  return <Bell />;
};

interface IInstallSectionProps {
  installed: boolean;
  canInstall: boolean;
  iosStepsShown: boolean;
}

/**
 * How to get the app on this device: the browser's own offer when it makes one, the Home Screen steps on an
 * iPhone, and otherwise the menu item every Chromium browser has, which installs any page.
 */
const InstallSection = ({ installed, canInstall, iosStepsShown }: IInstallSectionProps) => {
  if (installed)
    return (
      <p className="tw-notify-status" data-tone="ok">
        {t('agents.notificationControl.installed')}
      </p>
    );
  if (canInstall) {
    return (
      <>
        <p className="tw-notify-note">{t('agents.notificationControl.installNote')}</p>
        <div className="tw-notify-actions">
          <TwButton size={BUTTON_SIZE.sm} icon={<Smartphone />} onClick={() => void installApp()}>
            {t('agents.notificationControl.installApp')}
          </TwButton>
        </div>
      </>
    );
  }
  if (isTouchApple()) return iosStepsShown ? null : <InstallSteps />;
  return <p className="tw-notify-note">{t('agents.notificationControl.installHint')}</p>;
};

/** Per-device switch: permission and subscription belong to this browser, not to the account. A view of the bell's popover. */
const PushSettings = ({ onBack }: { onBack: () => void }) => {
  const state = usePushStore((s) => s.state);
  const busy = usePushStore((s) => s.busy);
  const [testing, setTesting] = useState(false);
  const canInstall = useInstallStore((s) => s.offer !== null);
  const installed = useRunsInstalled();

  const test = async () => {
    setTesting(true);
    const ok = await sendTestPush().catch(() => false);
    setTesting(false);
    if (!ok) toast.error(t('agents.notificationControl.testFailed'));
  };

  const enable = async () => {
    const result = await enablePush();
    if (result.state === 'on') void test();
    else if (result.state === 'denied')
      toast.error(t('agents.notificationControl.deniedToast', { host: appHost() }));
    else if (result.error) toast.error(t('agents.notificationControl.enableFailed', { error: result.error }));
  };

  return (
    <div className="tw-notify-card">
      <div className="tw-notify-head">
        <button
          type="button"
          className="tw-icon-button tw-notify-back"
          aria-label={t('agents.notificationControl.backToNotifications')}
          title={t('agents.notificationControl.back')}
          onClick={onBack}
        >
          <ArrowLeft />
        </button>
        <span className="tw-notify-icon" data-state={state}>
          <PushStateIcon state={state} />
        </span>
        <div>
          <strong>{t('agents.notificationControl.pushTitle')}</strong>
          <small>{t('agents.notificationControl.pushSubtitle')}</small>
        </div>
      </div>

      {state === 'on' && (
        <>
          <p className="tw-notify-status" data-tone="ok">
            {t('agents.notificationControl.onHere')}
          </p>
          <p className="tw-notify-note">{t('agents.notificationControl.onNote')}</p>
          <div className="tw-notify-actions">
            <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.primary} pending={testing} onClick={test}>
              {t('agents.notificationControl.sendTest')}
            </TwButton>
            <TwButton size={BUTTON_SIZE.sm} pending={busy} onClick={() => void disablePush()}>
              {t('agents.notificationControl.turnOff')}
            </TwButton>
          </div>
        </>
      )}

      {state === 'off' && (
        <>
          <p className="tw-notify-status">{t('agents.notificationControl.offHere')}</p>
          <div className="tw-notify-actions">
            <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.primary} pending={busy} onClick={enable}>
              {t('agents.notificationControl.turnOn')}
            </TwButton>
          </div>
        </>
      )}

      <InstallSection
        installed={installed}
        canInstall={canInstall}
        iosStepsShown={state === 'needs-install'}
      />

      {state === 'needs-install' && (
        <>
          <p className="tw-notify-status">{t('agents.notificationControl.iphoneNote')}</p>
          <InstallSteps />
        </>
      )}

      {state === 'denied' && (
        <>
          <p className="tw-notify-status" data-tone="warn">
            {t('agents.notificationControl.deniedStatus')}
          </p>
          <p className="tw-notify-note">
            {isAppleDevice()
              ? t('agents.notificationControl.deniedApple')
              : t('agents.notificationControl.deniedOther', { host: appHost() })}
          </p>
        </>
      )}

      {(state === 'unsupported' || state === 'unknown') && (
        <p className="tw-notify-note">{t('agents.notificationControl.unsupportedNote')}</p>
      )}
    </div>
  );
};

export default PushSettings;
