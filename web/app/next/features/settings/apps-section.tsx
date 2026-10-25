// «Apps»: the services the agent works with on the machine, as the Apps screen reads them. An app is connected where
// its address and token are kept — in the machine's environment — so the page says what it found and why an app is
// off; it has no connect button to press.
import { Group, SetButton } from '@/next/features/settings/rows';
import { APP_REGISTRY } from '@/next/features/apps/model';
import { t } from '@/next/i18n';
import type { AppFeed, AppsFeed } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { Button } from '@/next/ui/button';
import { Skeleton } from '@/next/ui/skeleton';
import Swap from '@/next/ui/swap';

export interface IAppsSectionProps {
  feed: AppsFeed | null;
  /** Why the machine could not be asked, in words. */
  error: string | null;
  onOpen: () => void;
  onRetry: () => void;
}

/** What an app says about itself when it is not ready: the same words as its screen. */
const stateText = (app: AppFeed): string => {
  const host = app.host ?? t(`next.apps.tab.${app.id}`);
  switch (app.reason) {
    case 'not-configured':
      return t(`next.apps.off.notConfigured.${app.id}`);
    case 'no-token':
      return t(`next.apps.off.noToken.${app.id}`, { host });
    case 'denied':
    case 'unreachable':
      return t(`next.apps.failed.${app.reason}`, { host });
    default:
      return t('next.settings.apps.ready');
  }
};

const AppsSection = ({ feed, error, onOpen, onRetry }: IAppsSectionProps) => (
  <>
    {error && (
      <Group>
        <div className="hx-set__empty">
          <p>{error}</p>
          <Button size="sm" icon="rotate-ccw" onClick={onRetry}>
            {t('next.settings.retry')}
          </Button>
        </div>
      </Group>
    )}
    {!error && (
      <Swap ready={feed !== null} skeleton={<Skeleton className="hx-set__sk" />}>
        {feed && (
          <div className="hx-apps">
            {APP_REGISTRY.map((tab) => {
              const app = feed.apps.find((item) => item.id === tab.id);
              if (!app) return null;
              const ready = app.state === 'ready';
              return (
                <div className="hx-app-card" key={tab.id} data-on={ready}>
                  <div className="hx-app-card__top">
                    <span className="hx-app-card__ic">
                      <Icon name={tab.icon} />
                    </span>
                    <div className="hx-app-card__text">
                      <div className="hx-app-card__name">{t(`next.apps.tab.${tab.id}`)}</div>
                      <div className="hx-app-card__by">
                        {app.host ?? t(`next.settings.apps.by.${tab.id}`)}
                      </div>
                    </div>
                  </div>
                  <div className="hx-app-card__desc">{t(`next.settings.apps.desc.${tab.id}`)}</div>
                  <div className="hx-app-card__state" data-ready={ready}>
                    {stateText(app)}
                  </div>
                  {ready && (
                    <div className="hx-app-card__foot">
                      <span className="hx-spacer" />
                      <SetButton tone="ghost" onClick={onOpen}>
                        {t('next.settings.apps.open')}
                      </SetButton>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </Swap>
    )}
    <div className="hx-set__foot">{t('next.settings.apps.foot')}</div>
  </>
);

export default AppsSection;
