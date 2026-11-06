// The installed apps, /apps: a grid of cards, each opens the app as the main thing, in the chat it was last on in (or
// alone). A card shows the app's state; its «details» button opens a side sheet with the version, the rights the app
// has and the switch of a team app (or of the organization's environment app). With no apps there is an empty state and the catalog under it.
import { useState, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import Sheet from '@/next/ui/sheet';
import Switch from '@/next/ui/switch';
import { Button, RoundButton } from '@/next/ui/button';
import { Skeleton } from '@/next/ui/skeleton';
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { ITeamState } from '@/next/features/app-catalog/use-team';
import { ROW_ACTION, rowsOf, type ICatalogRow } from '@/next/features/app-catalog/model';
import { t } from '@/next/i18n';
import '@/next/styles/app-main.css';

export interface IAppsListProps {
  /** null while they load. */
  apps: readonly IInstalledApp[] | null;
  error: string | null;
  onOpen: (app: IInstalledApp) => void;
  onOpenDrawer: () => void;
  /** «How to write an app»: the documentation. */
  onHelp: () => void;
  /** The team's apps and the person's switches of them; absent for a machine in no team. */
  team?: ITeamState;
  /** Under the list: the catalogs. */
  footer?: ReactNode;
  /** Turns the environment's app off (or on again) for the person; it rejects with the text to show. */
  onEnvMute?: (app: IInstalledApp, mute: boolean) => Promise<void>;
}

const stateOf = (app: IInstalledApp, row: ICatalogRow | null): string => {
  if (app.dev) return t('next.appslist.dev');
  if (app.env) return t('next.appslist.env');
  return row ? t('next.appslist.team') : t('next.appslist.mine');
};

/** The switch of the environment's app: a pinned one cannot be turned off, and says so. */
const EnvSwitch = ({
  app,
  onMute,
}: {
  app: IInstalledApp;
  onMute: (app: IInstalledApp, mute: boolean) => Promise<void>;
}) => {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  if (app.locked) return <p className="hx-appsheet__text">{t('next.appslist.envLocked')}</p>;
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
      <label className="hx-appsheet__switch">
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

/** The team's catalog row of an app, when the team has it. */
const teamRow = (team: ITeamState | undefined, app: IInstalledApp): ICatalogRow | null => {
  if (!team?.info || team.info.space === '') return null;
  return rowsOf(team.info).find((row) => row.entry.id === app.id) ?? null;
};

const AppCard = ({
  app,
  row,
  onOpen,
  onDetails,
}: {
  app: IInstalledApp;
  row: ICatalogRow | null;
  onOpen: () => void;
  onDetails: () => void;
}) => (
  <div className="hx-appcard" data-muted={row?.muted || app.muted || undefined}>
    <button type="button" className="hx-appcard__open" onClick={onOpen}>
      <span className="hx-appcard__tile">
        <Icon name={app.icon || 'layout-grid'} />
      </span>
      <span className="hx-appcard__name">{app.name}</span>
      {app.description && <span className="hx-appcard__desc">{app.description}</span>}
      <span className="hx-appcard__state">
        {stateOf(app, row)}
        {(row?.muted || app.muted) && ` · ${t('next.appslist.off')}`}
      </span>
    </button>
    <RoundButton
      icon="info"
      size="sm"
      variant="ghost"
      className="hx-appcard__more"
      label={t('next.appslist.details', { name: app.name })}
      onClick={onDetails}
    />
  </div>
);

const AppDetails = ({
  app,
  row,
  team,
  onClose,
  onEnvMute,
}: {
  app: IInstalledApp | null;
  row: ICatalogRow | null;
  team?: ITeamState;
  onClose: () => void;
  onEnvMute?: (app: IInstalledApp, mute: boolean) => Promise<void>;
}) => (
  <Sheet open={app !== null} onClose={onClose} label={app?.name ?? ''} className="hx-appsheet">
    {app && (
      <div className="hx-appsheet__in">
        <div className="hx-appsheet__head">
          <span className="hx-appcard__tile">
            <Icon name={app.icon || 'layout-grid'} />
          </span>
          <span className="hx-appsheet__title">
            <strong>{app.name}</strong>
            <span>
              {t('next.appslist.version', { version: app.version })} · {app.trust}
            </span>
          </span>
        </div>
        {app.description && <p className="hx-appsheet__text">{app.description}</p>}
        {app.env && onEnvMute && (
          <div className="hx-appsheet__sec">
            <EnvSwitch app={app} onMute={onEnvMute} />
          </div>
        )}
        {!app.env && row && team && row.action !== null && (
          <div className="hx-appsheet__sec">
            {team.info?.role === 'owner' ? (
              <Button
                variant={row.action === ROW_ACTION.install ? 'primary' : 'default'}
                busy={team.busy === app.id}
                onClick={() => void team.act(row)}
              >
                {t(row.action === ROW_ACTION.install ? 'next.appteam.install' : 'next.appteam.uninstall')}
              </Button>
            ) : (
              <label className="hx-appsheet__switch">
                <span>{t('next.appslist.enabled')}</span>
                <Switch
                  label={t('next.appslist.enabled')}
                  checked={!row.muted}
                  disabled={team.busy !== null}
                  onChange={() => void team.act(row)}
                />
              </label>
            )}
            {team.failure && (
              <p className="hx-appteam__fail" role="alert">
                {team.failure}
              </p>
            )}
          </div>
        )}
        <div className="hx-appsheet__sec">
          <h3 className="hx-appsheet__h">{t('next.inspector.rights')}</h3>
          {app.permissions.length === 0 ? (
            <p className="hx-appsheet__text">{t('next.inspector.noRights')}</p>
          ) : (
            <ul className="hx-appsheet__chips">
              {app.permissions.map((p) => (
                <li key={p}>{p}</li>
              ))}
            </ul>
          )}
        </div>
        {app.mcpServers.length > 0 && (
          <div className="hx-appsheet__sec">
            <h3 className="hx-appsheet__h">{t('next.appslist.servers')}</h3>
            <ul className="hx-appsheet__chips">
              {app.mcpServers.map((s) => (
                <li key={s}>{s}</li>
              ))}
            </ul>
          </div>
        )}
      </div>
    )}
  </Sheet>
);

const AppsList = ({ apps, error, onOpen, onOpenDrawer, onHelp, team, footer, onEnvMute }: IAppsListProps) => {
  const [details, setDetails] = useState<string | null>(null);
  const shown = apps?.find((a) => a.id === details) ?? null;
  let body = null;
  if (error) body = <p className="hx-appslist__none">{error}</p>;
  else if (apps === null) {
    body = (
      <div className="hx-appgrid" aria-busy="true">
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} className="hx-appcard hx-appcard--skel" />
        ))}
      </div>
    );
  } else if (apps.length === 0) {
    body = (
      <div className="hx-appslist__empty">
        <span className="hx-appcard__tile">
          <Icon name="layout-grid" />
        </span>
        <p className="hx-appslist__none">{t('next.appslist.none')}</p>
        <p className="hx-appslist__hint">{t('next.appslist.catalog')}</p>
      </div>
    );
  } else {
    body = (
      <div className="hx-appgrid">
        {apps.map((app) => (
          <AppCard
            key={app.id}
            app={app}
            row={teamRow(team, app)}
            onOpen={() => onOpen(app)}
            onDetails={() => setDetails(app.id)}
          />
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
      <button type="button" className="hx-appslist__help" onClick={onHelp}>
        <Icon name="book-open" size="sm" />
        {t('next.appslist.help')}
      </button>
      {footer}
      <AppDetails
        app={shown}
        row={shown ? teamRow(team, shown) : null}
        team={team}
        onClose={() => setDetails(null)}
        onEnvMute={onEnvMute}
      />
    </div>
  );
};

export default AppsList;
