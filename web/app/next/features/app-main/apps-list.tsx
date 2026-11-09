// The installed apps, /apps: a grid of cards, each opens the page of the app. A card says in words where the app works
// and, only when the person has to act, what is missing. The header holds the search over all the catalogs and
// «Создать приложение»; the debug and safe mode switches sit in its «…» menu. With no apps there is an empty state and
// a few catalog apps under it.
import type { ReactNode } from 'react';
import { Menu } from '@base-ui/react/menu';
import MenuPanel, { useMenuActions } from '@/next/ui/menu-panel';
import AppTile, { whereOf } from '@/next/features/app-catalog/app-tile';
import { matches } from '@/next/features/app-catalog/apps-catalog';
import { toast } from '@/next/ui/toast';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';
import { rowsOf, type ICatalogRow } from '@/next/features/app-catalog/model';
import { Button } from '@/next/ui/button';
import { Skeleton } from '@/next/ui/skeleton';
import { APP_REASON, APP_STATE, type IInstalledApp } from '@/next/features/app-views/api';
import type { ITeamState } from '@/next/features/app-catalog/use-team';
import { t } from '@/next/i18n';
import { setAppsDebug, useAppsDebug } from '@/next/features/app-views/debug-mode';
import '@/next/styles/app-main.css';

export interface IAppsListProps {
  /** null while they load. */
  apps: readonly IInstalledApp[] | null;
  error: string | null;
  onOpen: (app: IInstalledApp) => void;
  onOpenDrawer: () => void;
  /** «How to write an app»: the documentation. */
  onHelp: () => void;
  /** «Создать приложение»: a new chat with the agent and the skill of apps. */
  onCreate: () => void;
  /** The header's search over the apps and the catalogs; the catalogs under the list read the same words. */
  query: string;
  onQuery: (query: string) => void;
  /** The catalogs have something for the search: «nothing found» is not said then. */
  elsewhere?: boolean;
  /** The team's apps and the person's switches of them; absent for a machine in no team. */
  team?: ITeamState;
  /** Under the list: the catalogs. */
  footer?: ReactNode;
  /** Safe mode, when the machine tells it: new chats start without the apps that are not pinned. */
  safeMode?: boolean;
  /** Turns safe mode on or off; it rejects with the text to show. */
  onSafeMode?: (on: boolean) => Promise<void>;
}

/** The app or a newer version of it waits for the person's word on what it reaches. */
export const waitsConsent = (app: Pick<IInstalledApp, 'consent' | 'pending'>): boolean =>
  app.consent === true || app.pending !== undefined;

/** Why the app does nothing now, in words; null when it is ready or only turned off (said by its switch). */
export const reasonOf = (app: Pick<IInstalledApp, 'state' | 'reason' | 'detail'>): string | null => {
  if (app.reason === APP_REASON.safe) return t('next.appslist.reason.safe');
  if (app.reason === APP_REASON.consent) return t('next.consent.waits');
  if (app.state === undefined || app.state === APP_STATE.ready || app.state === APP_STATE.disabled)
    return null;
  const detail = app.detail ?? '';
  switch (app.reason) {
    case APP_REASON.secret:
      return t('next.appslist.reason.secret', { names: detail });
    case APP_REASON.connection:
      return t('next.appslist.reason.connection', { names: detail });
    case APP_REASON.host:
      return t('next.appslist.reason.host', { range: detail });
    case APP_REASON.package:
      return t('next.appslist.reason.package');
    default:
      return null;
  }
};

/** The header's «…»: the switches for the people who build apps, out of the way. */
const MoreMenu = ({
  safeMode,
  onSafeMode,
  onHelp,
}: {
  onHelp: () => void;
  safeMode?: boolean;
  onSafeMode?: (on: boolean) => Promise<void>;
}) => {
  const actions = useMenuActions();
  const debug = useAppsDebug();
  const safe = async (on: boolean) => {
    try {
      await onSafeMode?.(on);
    } catch (err) {
      toast(err instanceof Error ? err.message : t('next.appslist.envFailed'), { tone: 'error' });
    }
  };
  return (
    <Menu.Root actionsRef={actions}>
      <Menu.Trigger className="hx-head-btn hx-appslist__more" aria-label={t('next.appslist.more')}>
        <Icon name="ellipsis" />
      </Menu.Trigger>
      <MenuPanel
        actions={actions}
        className="hx-cmenu hx-cmenu--switch"
        label={t('next.appslist.more')}
        side="bottom"
        align="end"
        sideOffset={6}
      >
        <Menu.CheckboxItem
          className="hx-cmenu__item hx-cmenu__item--two"
          checked={debug}
          closeOnClick={false}
          onCheckedChange={(on) => setAppsDebug(on)}
        >
          <span className="hx-cmenu__txt">
            {t('next.appslist.debug')}
            <span className="hx-cmenu__hint">{t('next.appslist.debugHint')}</span>
          </span>
          <Switch label={t('next.appslist.debug')} checked={debug} onChange={() => undefined} />
        </Menu.CheckboxItem>
        <Menu.Item className="hx-cmenu__item" onClick={onHelp}>
          <Icon name="book-open" size="sm" />
          <span className="hx-cmenu__txt">{t('next.appslist.help')}</span>
        </Menu.Item>
        {safeMode !== undefined && onSafeMode && (
          <Menu.CheckboxItem
            className="hx-cmenu__item hx-cmenu__item--two"
            checked={safeMode}
            closeOnClick={false}
            onCheckedChange={(on) => void safe(on)}
          >
            <span className="hx-cmenu__txt">
              {t('next.appslist.safe')}
              <span className="hx-cmenu__hint">{t('next.appslist.safeHint')}</span>
            </span>
            <Switch label={t('next.appslist.safe')} checked={safeMode} onChange={() => undefined} />
          </Menu.CheckboxItem>
        )}
      </MenuPanel>
    </Menu.Root>
  );
};

/** The team's catalog row of an app, when the team has it. */
const teamRow = (team: ITeamState | undefined, app: IInstalledApp): ICatalogRow | null => {
  if (!team?.info || team.info.space === '') return null;
  return rowsOf(team.info).find((row) => row.entry.id === app.id) ?? null;
};

const AppCard = ({
  app,
  row,
  onOpen,
}: {
  app: IInstalledApp;
  row: ICatalogRow | null;
  onOpen: () => void;
}) => (
  <AppTile
    icon={app.icon}
    name={app.name}
    line={app.description}
    where={whereOf(app.targets)}
    note={app.pending !== undefined ? t('next.consent.reviewUpdate') : reasonOf(app)}
    muted={row?.muted || app.muted || app.off}
    onOpen={onOpen}
  />
);

const AppsList = ({
  apps,
  error,
  onOpen,
  onOpenDrawer,
  onHelp,
  onCreate,
  query,
  onQuery,
  elsewhere = false,
  team,
  footer,
  safeMode,
  onSafeMode,
}: IAppsListProps) => {
  const found = (apps ?? []).filter((a) => matches(query, a.name, a.description));
  const searching = query.trim() !== '';
  let body = null;
  if (error) body = <p className="hx-appslist__none">{error}</p>;
  else if (apps === null) {
    body = (
      <div className="hx-appk-grid" aria-busy="true">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="hx-appk hx-appk--skel" />
        ))}
      </div>
    );
  } else if (apps.length === 0) {
    body = (
      <div className="hx-appslist__empty">
        <h2 className="hx-appslist__lead">{t('next.appslist.none')}</h2>
        <p className="hx-appslist__hint">{t('next.appslist.noneHint')}</p>
        <Button icon="book-open" onClick={onHelp}>
          {t('next.appslist.howOwn')}
        </Button>
        <details className="hx-appslist__dev">
          <summary>{t('next.appslist.devHint')}</summary>
          <code>harnessly app install ./app</code>
        </details>
      </div>
    );
  } else if (found.length > 0) {
    body = (
      <section className="hx-appsec hx-appsec--first" aria-label={t('next.appslist.mine')}>
        <h2 className="hx-appsec__h2">{t('next.appslist.mine')}</h2>
        <div className="hx-appk-grid">
          {found.map((app) => (
            <AppCard key={app.id} app={app} row={teamRow(team, app)} onOpen={() => onOpen(app)} />
          ))}
        </div>
      </section>
    );
  } else if (searching && !elsewhere) {
    body = <p className="hx-appslist__none">{t('next.appslist.nothing', { query: query.trim() })}</p>;
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
        <label className="hx-appslist__search">
          <Icon name="search" size="sm" />
          <input
            type="search"
            value={query}
            placeholder={t('next.appslist.search')}
            aria-label={t('next.appslist.search')}
            onChange={(e) => onQuery(e.target.value)}
          />
        </label>
        <Button
          variant="primary"
          icon="plus"
          className="hx-appslist__create"
          aria-label={t('next.appslist.create')}
          onClick={onCreate}
        >
          {t('next.appslist.create')}
        </Button>
        <MoreMenu safeMode={safeMode} onSafeMode={onSafeMode} onHelp={onHelp} />
      </div>
      {safeMode && <p className="hx-appslist__hint hx-appslist__safe-on">{t('next.appslist.safeOn')}</p>}
      {body}
      {footer}
    </div>
  );
};

export default AppsList;
