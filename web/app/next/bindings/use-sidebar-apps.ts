// «Приложения» of the sidebar: harnessly's own apps (the «Ревью кода»), then the installed apps that are on in the
// person's chats, each opening in the chat it was last on in, the organization's environment apps with a page of their
// own, and the way to the whole list.
import { useMemo } from 'react';
import { prepareViewSession, useInstalledApps, type IInstalledApp } from '@/next/features/app-views/api';
import { menuApps, sidebarApps, type ISidebarApp } from '@/next/features/app-main/chat-apps';
import { appOfKey } from '@/next/features/sidebar/nav-layout';
import { useMe, type NavSettings } from '@/next/stores/me';
import { isSpaceApp } from '@/next/features/app-views/service-health';
import type { ISidebarApps } from '@/next/features/sidebar/sidebar';
import type { Conversation, MachineId } from '@/next/protocol/v2';
import { ROUTE, type TRoute } from '@/next/routes/routes';
import { t } from '@/next/i18n';

/** The apps of the menu with the ones the person ordered or raised (Settings → «Меню слева»), on in a chat or not. */
const withNamed = (
  listed: ISidebarApp[],
  installed: readonly IInstalledApp[],
  nav: NavSettings | null,
): ISidebarApp[] => {
  if (nav === null) return listed;
  const named = new Set([...nav.apps, ...nav.order.map(appOfKey).filter((id): id is string => id !== null)]);
  const have = new Set(listed.map((x) => x.app.id));
  return [
    ...listed,
    ...menuApps(installed)
      .filter((a) => named.has(a.id) && !have.has(a.id))
      .map((app) => ({ app })),
  ];
};

export const useSidebarApps = (
  machine: MachineId | null,
  conversations: readonly Conversation[] | null,
  route: TRoute,
  open: (to: TRoute) => void,
): ISidebarApps | null => {
  // Read always: the environment's apps are in the menu even when no chat has an app on.
  const installed = useInstalledApps(machine ?? 'local', 0, machine !== null).list;
  const me = useMe();
  const nav = me.k === 'ready' ? me.me.settings.nav : null;
  const rows = useMemo(
    () =>
      installed === null || conversations === null
        ? []
        : withNamed(sidebarApps(conversations, installed.apps), installed.apps, nav).map(({ app, chat }) => ({
            id: app.id,
            name: app.name,
            ...(app.icon ? { icon: app.icon } : {}),
            // A space (boards, documents) opens on its own, never in the chat it was last on in.
            ...(chat === undefined || isSpaceApp(app.id) ? {} : { chat }),
          })),
    [installed, conversations, nav],
  );
  return useMemo(
    () =>
      machine === null
        ? null
        : {
            builtins: [
              {
                id: 'review',
                name: t('next.reviewApp.title'),
                icon: 'git-pull-request',
                current: route.kind === ROUTE.review,
                onOpen: () => open({ kind: ROUTE.review }),
              },
            ],
            rows,
            current: route.kind === ROUTE.app ? route.id : null,
            allCurrent: route.kind === ROUTE.apps,
            onOpen: (id, chat) => open({ kind: ROUTE.app, id, ...(chat === undefined ? {} : { chat }) }),
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
                conversation: chat ?? null,
              });
            },
            onAll: () => open({ kind: ROUTE.apps }),
          },
    [machine, rows, route, open, installed],
  );
};
