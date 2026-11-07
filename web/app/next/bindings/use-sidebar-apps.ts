// «Приложения» of the sidebar: the installed apps that are on in the person's chats, each opening in the chat it was
// last on in, and the way to the whole list. The installed list is read only once some chat has an app on.
import { useMemo } from 'react';
import { prepareViewSession, useInstalledApps } from '@/next/features/app-views/api';
import { activeApps } from '@/next/features/app-main/chat-apps';
import type { ISidebarApps } from '@/next/features/sidebar/sidebar';
import type { Conversation, MachineId } from '@/next/protocol/v2';
import { ROUTE, type TRoute } from '@/next/routes/routes';

export const useSidebarApps = (
  machine: MachineId | null,
  conversations: readonly Conversation[] | null,
  route: TRoute,
  open: (to: TRoute) => void,
): ISidebarApps | null => {
  const anyOn = conversations?.some((c) => (c.apps?.length ?? 0) > 0) ?? false;
  const installed = useInstalledApps(machine ?? 'local', 0, machine !== null && anyOn).list;
  const rows = useMemo(
    () =>
      installed === null || conversations === null
        ? []
        : activeApps(conversations, installed.apps).map(({ app, chat }) => ({
            id: app.id,
            name: app.name,
            chat,
          })),
    [installed, conversations],
  );
  return useMemo(
    () =>
      machine === null
        ? null
        : {
            rows,
            current: route.kind === ROUTE.app ? route.id : null,
            allCurrent: route.kind === ROUTE.apps,
            onOpen: (id, chat) => open({ kind: ROUTE.app, id, chat }),
            onPrepare: (id, chat) => {
              // The same view and target the app's own page picks (app-main): its `app` view, else its pane view.
              const app = installed?.apps.find((a) => a.id === id);
              const own = app?.views.find((v) => v.targets.includes('app'));
              const view = own ?? app?.views.find((v) => v.targets.includes('pane'));
              if (view?.path === undefined && view?.url === undefined) return;
              prepareViewSession(machine, {
                app: id,
                view: view.id,
                target: own ? 'app' : 'pane',
                conversation: chat,
              });
            },
            onAll: () => open({ kind: ROUTE.apps }),
          },
    [machine, rows, route, open, installed],
  );
};
