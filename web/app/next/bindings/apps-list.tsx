// «Приложения»: the installed apps of the machine. Opening one takes it to the chat it was last on in, so the app
// opens with its chat beside it; an app on in no chat opens alone.
import { useState } from 'react';
import { setSafeMode, useInstalledApps, type IInstalledApp } from '@/next/features/app-views/api';
import AppsCatalog from '@/next/features/app-catalog/apps-catalog';
import PublicCatalog from '@/next/features/app-catalog/public-catalog';
import { useTeam } from '@/next/features/app-catalog/use-team';
import { muteTeamApp } from '@/next/features/app-catalog/api';
import { usePublicCatalog } from '@/next/features/app-catalog/use-public';
import { activeApps } from '@/next/features/app-main/chat-apps';
import AppsList from '@/next/features/app-main/apps-list';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { useConversationList } from '@/next/stores/conversations';

const AppsListPage = ({ machine, onOpenDrawer }: { machine: MachineId; onOpenDrawer: () => void }) => {
  // The team's apps are in the list too: a change of the team's catalog reads the list again.
  const [reload, setReload] = useState(0);
  const { list, error } = useInstalledApps(machine, reload);
  const onChanged = () => setReload((n) => n + 1);
  const team = useTeam(machine, onChanged);
  const catalog = usePublicCatalog(machine, onChanged);
  const chats = useConversationList(machine);
  const muteEnv = async (app: IInstalledApp, mute: boolean) => {
    await muteTeamApp(machine, app.id, mute);
    onChanged();
  };
  const safe = async (on: boolean) => {
    await setSafeMode(machine, on);
    onChanged();
  };
  const open = (app: IInstalledApp) => {
    const chat = activeApps(chats ?? [], [app])[0]?.chat;
    goTo({ kind: ROUTE.app, id: app.id, ...(chat ? { chat } : {}) });
  };
  return (
    <AppsList
      apps={list?.apps ?? null}
      error={error}
      onOpen={open}
      onOpenDrawer={onOpenDrawer}
      onHelp={() => goTo({ kind: ROUTE.help, section: 'build-apps', page: 'overview' })}
      team={team}
      onEnvMute={muteEnv}
      safeMode={list?.safeMode}
      onSafeMode={safe}
      footer={
        <>
          <AppsCatalog team={team} />
          <PublicCatalog catalog={catalog} installed={(list?.apps ?? []).map((a) => a.id)} />
        </>
      }
    />
  );
};

export default AppsListPage;
