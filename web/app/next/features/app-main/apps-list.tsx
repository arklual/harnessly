// The installed apps, /apps: each opens as the main thing, in the chat it was last on in (or alone). Nothing but the
// name and what the app is for.
import type { ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import type { IInstalledApp } from '@/next/features/app-views/api';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

export interface IAppsListProps {
  /** null while they load. */
  apps: readonly IInstalledApp[] | null;
  error: string | null;
  onOpen: (app: IInstalledApp) => void;
  onOpenDrawer: () => void;
  /** Under the list: the team's catalog. */
  footer?: ReactNode;
}

const AppsList = ({ apps, error, onOpen, onOpenDrawer, footer }: IAppsListProps) => {
  let body = null;
  if (error) body = <p className="hx-appslist__none">{error}</p>;
  else if (apps === null) body = <div className="hx-appview__wait" aria-busy="true" />;
  else if (apps.length === 0) body = <p className="hx-appslist__none">{t('next.appslist.none')}</p>;
  else {
    body = (
      <div className="hx-appslist__rows">
        {apps.map((app) => (
          <button key={app.id} type="button" className="hx-appslist__row" onClick={() => onOpen(app)}>
            <Icon name="layout-grid" />
            <span className="hx-appslist__text">
              <span className="hx-appslist__name">{app.name}</span>
              {app.description && <span className="hx-appslist__desc">{app.description}</span>}
            </span>
          </button>
        ))}
      </div>
    );
  }
  return (
    <div className="hx-appslist">
      <div className="hx-appslist__bar">
        <button
          type="button"
          className="hx-head-btn hx-appmain__menu"
          aria-label={t('next.header.openMenu')}
          onClick={onOpenDrawer}
        >
          <Icon name="menu" />
        </button>
        <h1 className="hx-appslist__h1">{t('next.sidebar.apps')}</h1>
      </div>
      {body}
      {footer}
    </div>
  );
};

export default AppsList;
