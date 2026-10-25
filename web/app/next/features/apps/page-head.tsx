// The top of an app's page: the way back to the list (on a phone), the app's name and the service it is read from, and
// the page's own buttons.
import type { ReactNode } from 'react';
import { appIcon } from '@/next/features/apps/model';
import { t } from '@/next/i18n';
import type { AppId } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import Tooltip from '@/next/ui/tooltip';

export interface IPageHeadProps {
  app: AppId;
  host: string | null;
  /** True while the machine is asked again: the refresh button turns. */
  refreshing: boolean;
  onBack: () => void;
  onRefresh: () => void;
  /** The page's own buttons, before the refresh. */
  children?: ReactNode;
}

const PageHead = ({ app, host, refreshing, onBack, onRefresh, children }: IPageHeadProps) => (
  <div className="hx-hubhead">
    <button
      type="button"
      className="hx-head-btn hx-hubhead__back"
      aria-label={t('next.apps.back')}
      onClick={onBack}
    >
      <Icon name="chevron-left" />
    </button>
    <span className="hx-hubhead__mark" aria-hidden="true">
      <Icon name={appIcon(app)} />
    </span>
    <div className="hx-hubhead__titles">
      <h2 className="hx-hubhead__title">{t(`next.apps.tab.${app}`)}</h2>
      {host && (
        <a className="hx-hubhead__host" href={`https://${host}`} target="_blank" rel="noreferrer">
          {host}
          <Icon name="arrow-up-right" size="sm" />
        </a>
      )}
    </div>
    <span className="hx-spacer" />
    {children}
    <Tooltip label={t('next.apps.refresh')}>
      <button
        type="button"
        className="hx-head-btn"
        data-busy={refreshing || undefined}
        aria-label={t('next.apps.refresh')}
        onClick={onRefresh}
      >
        <Icon name="refresh-cw" size="sm" />
      </button>
    </Tooltip>
  </div>
);

export default PageHead;
