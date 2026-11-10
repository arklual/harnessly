// The page of an app, /apps/:id/about: an installed app, or one of the team's or the public catalog (not installed yet).
// «Открыть» goes to the app with the chat it was last on in; «Поставить» is the team's install for an owner, or the
// install screen of the public catalog. What a waiting install or update reaches is the shared consent component.
import { useState, type ReactNode } from 'react';
import AppPage, { abilitiesOf } from '@/next/features/app-main/app-page';
import { EnvSwitch, OwnSwitch } from '@/next/features/app-main/app-switches';
import { reasonOf, waitsConsent } from '@/next/features/app-main/apps-list';
import { activeApps } from '@/next/features/app-main/chat-apps';
import AccessList, { type IRevoke } from '@/next/features/app-consent/access-list';
import ConsentBlock from '@/next/features/app-consent/consent-block';
import ConsentSheet, { type IConsentApp } from '@/next/features/app-consent/consent-sheet';
import AppFrame from '@/next/features/app-views/app-frame';
import { Journal } from '@/next/features/app-views/inspector';
import {
  APP_STATE,
  consentApp,
  declineApp,
  revokeAccess,
  switchApp,
  useInstalledApps,
  type IAccess,
  type IInstalledApp,
} from '@/next/features/app-views/api';
import { muteTeamApp, type IPublicEntry } from '@/next/features/app-catalog/api';
import { refusalOf, ROW_ACTION, rowsOf, secretsOf } from '@/next/features/app-catalog/model';
import { useTeam } from '@/next/features/app-catalog/use-team';
import { usePublicCatalog } from '@/next/features/app-catalog/use-public';
import { Button } from '@/next/ui/button';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { useConversationList } from '@/next/stores/conversations';
import { useTabTitle } from '@/next/stores/tabs';
import { t } from '@/next/i18n';

interface IAsk {
  id: string;
  /** The public entry to install; absent for the consent of an app that is installed. */
  entry?: IPublicEntry;
  app: IConsentApp;
  access: readonly IAccess[];
  update: boolean;
}

/** What an entry of a catalog reaches in words; an older machine gives only the rights' names, shown as they are. */
const accessOfEntry = (entry: IPublicEntry): IAccess[] =>
  entry.check?.access ??
  entry.permissions.map((p) => ({ key: `perm:${p}`, code: p, class: 'medium', label: { ru: p, en: p } }));

const Rights = ({
  access,
  permissions,
  revoke,
}: {
  access?: readonly IAccess[];
  permissions: readonly string[];
  /** HRN-965: every access the app is given can be taken back alone. */
  revoke?: IRevoke;
}) => {
  if (access) return <AccessList access={access} revoke={revoke} />;
  if (permissions.length === 0) return <p className="hx-apppage__none">{t('next.inspector.noRights')}</p>;
  return (
    <ul className="hx-apppage__chips">
      {permissions.map((p) => (
        <li key={p}>{p}</li>
      ))}
    </ul>
  );
};

const AppAboutPage = ({
  machine,
  id,
  onOpenDrawer,
}: {
  machine: MachineId;
  id: string;
  onOpenDrawer: () => void;
}) => {
  const [reload, setReload] = useState(0);
  const onChanged = () => setReload((n) => n + 1);
  const { list, error } = useInstalledApps(machine, reload);
  const team = useTeam(machine, onChanged);
  const catalog = usePublicCatalog(machine, onChanged);
  const chats = useConversationList(machine);
  // The install screen: a first install or an update that waits, or an install from the public catalog. It keeps what it
  // showed while it leaves.
  const [ask, setAsk] = useState<IAsk | null>(null);
  const [asking, setAsking] = useState(false);
  const askFor = (next: IAsk) => {
    setAsk(next);
    setAsking(true);
  };

  const app = list?.apps.find((a) => a.id === id) ?? null;
  const row =
    team.info && team.info.space !== '' ? (rowsOf(team.info).find((r) => r.entry.id === id) ?? null) : null;
  const offered = catalog.entries?.find((e) => e.id === id) ?? null;
  useTabTitle(routePath({ kind: ROUTE.appAbout, id }), app?.name ?? row?.entry.name ?? offered?.name ?? null);

  const back = () => goTo({ kind: ROUTE.apps });
  const loaded =
    list !== null &&
    (team.info !== null || team.error !== null) &&
    (catalog.entries !== null || catalog.error !== null);
  const shared = { onBack: back, onOpenDrawer };
  if (error) {
    return (
      <AppPage
        {...shared}
        missing
        name=""
        actions={null}
        preview={null}
        can={[]}
        rights={null}
        manage={null}
        journal={null}
      />
    );
  }
  if (app === null && row === null && offered === null) {
    return (
      <AppPage
        {...shared}
        loading={!loaded}
        missing={loaded}
        name=""
        actions={null}
        preview={null}
        can={[]}
        rights={null}
        manage={null}
        journal={null}
      />
    );
  }

  const waiting = app !== null && waitsConsent(app);
  const sheet = ask && (
    <ConsentSheet
      open={asking}
      app={ask.app}
      access={ask.access}
      update={ask.update}
      onConfirm={async () => {
        if (ask.entry) await catalog.install(ask.entry);
        else {
          await consentApp(
            machine,
            ask.id,
            ask.app.version,
            ask.access.map((a) => a.key),
          );
          onChanged();
        }
        setAsking(false);
      }}
      onCancel={() => {
        if (!ask.entry) void declineApp(machine, ask.id).then(onChanged);
        setAsking(false);
      }}
      onClose={() => setAsking(false)}
    />
  );

  const open = (a: IInstalledApp) => {
    const chat = activeApps(chats ?? [], [a])[0]?.chat;
    goTo({ kind: ROUTE.app, id: a.id, ...(chat ? { chat } : {}) });
  };

  if (app !== null) {
    const canOpen = app.views.some((v) => v.targets.includes('app') || v.targets.includes('pane'));
    const view =
      app.views.find((v) => v.targets.includes('app')) ?? app.views.find((v) => v.targets.includes('pane'));
    const runs = !waiting && !app.off && (app.state === undefined || app.state === APP_STATE.ready);
    const preview =
      runs && view && list ? (
        <AppFrame
          machine={machine}
          app={app}
          view={view}
          target={view.targets.includes('app') ? 'app' : 'pane'}
          viewPort={list.viewPort}
          conversation={null}
        />
      ) : null;
    let actions: ReactNode = null;
    if (waiting) {
      actions = (
        <Button
          variant="primary"
          onClick={() =>
            askFor({
              id: app.id,
              app: { ...app, version: app.pending?.version ?? app.version },
              access: (app.pending ? app.pending.access : app.access) ?? [],
              update: app.pending !== undefined,
            })
          }
        >
          {app.pending ? t('next.consent.reviewUpdate') : t('next.consent.review')}
        </Button>
      );
    } else if (canOpen) {
      actions = (
        <Button variant="primary" onClick={() => open(app)}>
          {t('next.apppage.open')}
        </Button>
      );
    }
    const switches = [
      !app.env && !row && !app.consent ? (
        <OwnSwitch
          key="own"
          app={app}
          onSwitch={async (a, on) => {
            await switchApp(machine, a.id, on);
            onChanged();
          }}
        />
      ) : null,
      app.env ? (
        <EnvSwitch
          key="env"
          app={app}
          onMute={async (a, mute) => {
            await muteTeamApp(machine, a.id, mute);
            onChanged();
          }}
        />
      ) : null,
      !app.env && row && row.action !== null ? (
        <Button
          key="team"
          variant={row.action === ROW_ACTION.install ? 'primary' : 'default'}
          busy={team.busy === app.id}
          onClick={() => void team.act(row)}
        >
          {t(row.action === ROW_ACTION.uninstall ? 'next.appteam.uninstall' : 'next.appteam.install')}
        </Button>
      ) : null,
    ].filter((x) => x !== null);
    const manage =
      switches.length > 0 || team.failure ? (
        <>
          {switches}
          {team.failure && (
            <p className="hx-appteam__fail" role="alert">
              {team.failure}
            </p>
          )}
        </>
      ) : null;
    return (
      <>
        <AppPage
          {...shared}
          name={app.name}
          icon={app.icon}
          line={app.description}
          version={app.version}
          by={row?.entry.by}
          note={app.pending !== undefined ? t('next.consent.reviewUpdate') : reasonOf(app)}
          actions={actions}
          preview={preview}
          can={abilitiesOf(app)}
          rights={
            <>
              {app.pending !== undefined && <ConsentBlock app={app} />}
              <Rights
                access={app.access}
                permissions={app.permissions}
                revoke={
                  !app.env && !row && app.granted
                    ? {
                        granted: app.granted,
                        onRevoke: async (key) => {
                          await revokeAccess(machine, app.id, key);
                          onChanged();
                        },
                      }
                    : undefined
                }
              />
              {app.mcpServers.length > 0 && (
                <ul className="hx-apppage__chips">
                  {app.mcpServers.map((s) => (
                    <li key={s}>{s}</li>
                  ))}
                </ul>
              )}
            </>
          }
          manage={manage}
          journal={app.dev ? <Journal machine={machine} app={app.id} /> : null}
        />
        {sheet}
      </>
    );
  }

  // Not installed: the team's entry (an owner installs it for the team) or the public catalog's (the install screen).
  const entry = row?.entry ?? offered;
  if (entry === null) return null;
  const refused = refusalOf(entry.check);
  const ownerInstall = row !== null && row.action === ROW_ACTION.install;
  let actions: ReactNode = null;
  if (refused === null && ownerInstall) {
    actions = (
      <Button variant="primary" busy={team.busy === entry.id} onClick={() => void team.act(row)}>
        {t('next.appteam.install')}
      </Button>
    );
  } else if (refused === null && row === null) {
    actions = (
      <Button
        variant="primary"
        busy={catalog.busy === entry.id}
        onClick={() =>
          askFor({
            id: entry.id,
            entry: offered ?? undefined,
            app: { name: entry.name, version: entry.version, source: t('next.apppublic.title') },
            access: offered ? accessOfEntry(offered) : [],
            update: false,
          })
        }
      >
        {t('next.apppublic.install')}
      </Button>
    );
  }
  const failure = team.failure ?? catalog.failure;
  return (
    <>
      <AppPage
        {...shared}
        name={entry.name}
        line={entry.description}
        version={entry.version}
        by={entry.by}
        note={refused ?? secretsOf(entry.check)}
        actions={actions}
        preview={null}
        can={[]}
        rights={
          <>
            <Rights access={entry.check?.access} permissions={entry.permissions} />
            {failure && (
              <p className="hx-appteam__fail" role="alert">
                {failure}
              </p>
            )}
          </>
        }
        manage={null}
        journal={null}
      />
      {sheet}
    </>
  );
};

export default AppAboutPage;
