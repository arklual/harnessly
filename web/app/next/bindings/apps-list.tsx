// «Приложения»: the installed apps of the machine. A card opens the page of the app.
import { useState } from 'react';
import { setSafeMode, useInstalledApps } from '@/next/features/app-views/api';
import AppsCatalog from '@/next/features/app-catalog/apps-catalog';
import PublicCatalog from '@/next/features/app-catalog/public-catalog';
import { useTeam } from '@/next/features/app-catalog/use-team';
import { usePublicCatalog } from '@/next/features/app-catalog/use-public';
import AppsList from '@/next/features/app-main/apps-list';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { NEW_CHAT, setDraftText } from '@/next/stores/drafts';
import { matches } from '@/next/features/app-catalog/apps-catalog';
import { rowsOf } from '@/next/features/app-catalog/model';
import { t } from '@/next/i18n';

const AppsListPage = ({ machine, onOpenDrawer }: { machine: MachineId; onOpenDrawer: () => void }) => {
  // The team's apps are in the list too: a change of the team's catalog reads the list again.
  const [reload, setReload] = useState(0);
  // The header's search: the installed apps and both catalogs read it.
  const [query, setQuery] = useState('');
  const { list, error } = useInstalledApps(machine, reload);
  const onChanged = () => setReload((n) => n + 1);
  const team = useTeam(machine, onChanged);
  const catalog = usePublicCatalog(machine, onChanged);
  const safe = async (on: boolean) => {
    await setSafeMode(machine, on);
    onChanged();
  };
  // «Создать приложение»: a new chat whose field already asks the agent to use the skill of apps.
  const create = () => {
    setDraftText(NEW_CHAT, t('next.appslist.createDraft'));
    goTo({ kind: ROUTE.home });
  };
  const open = (id: string) => goTo({ kind: ROUTE.appAbout, id });
  const mine = list?.apps ?? null;
  const starter = mine !== null && mine.length === 0 && query.trim() === '';
  const teamHits = team.info && team.info.space !== '' ? rowsOf(team.info) : [];
  const elsewhere =
    teamHits.some((r) => !r.entry.installed && matches(query, r.entry.name, r.entry.description)) ||
    (catalog.entries ?? []).some((e) => !(mine ?? []).some((a) => a.id === e.id));
  return (
    <AppsList
      query={query}
      onQuery={(q) => {
        setQuery(q);
        catalog.setQuery(q);
      }}
      elsewhere={elsewhere}
      onCreate={create}
      apps={list?.apps ?? null}
      error={error}
      onOpen={(app) => open(app.id)}
      onOpenDrawer={onOpenDrawer}
      onHelp={() => goTo({ kind: ROUTE.help, section: 'build-apps', page: 'overview' })}
      team={team}
      safeMode={list?.safeMode}
      onSafeMode={safe}
      footer={
        <>
          <AppsCatalog team={team} query={query} limit={starter ? 3 : undefined} onOpen={open} />
          <PublicCatalog
            catalog={catalog}
            installed={(list?.apps ?? []).map((a) => a.id)}
            limit={starter ? 3 : undefined}
            onOpen={open}
            ask={list?.permissionsOff !== true}
          />
        </>
      }
    />
  );
};

export default AppsListPage;
