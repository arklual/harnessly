// An app opened as the main thing, /apps/<id>[?o=<object>][&chat=<conv>]: Sinopia's stage (features/app-main/
// app-stage.tsx) with the app's `app` view where a design has its page — the app's home, or one of its objects (a
// document, a board) the view opened with `hl.pane.open('object:<slug>')`. The chat is Sinopia's: bound to the app's
// home or to the object, docked, in a window or folded into the field, with the right pane of the chat beside it. The
// first words sent from the field or from the view (`hl.chat.send`) make the chat, with the app on in it. The strip's
// «…» holds the app's rights, the debug section of a dev install and turning the app off in the chat.
import { useCallback, useEffect, useMemo, useState } from 'react';
import AppAsks from '@/next/features/app-views/asks';
import AppFrame, { fromApp } from '@/next/features/app-views/app-frame';
import { ConsentStage } from '@/next/features/app-consent/consent-ask';
import { useInstalledApps, type IAppView, type IInstalledApp } from '@/next/features/app-views/api';
import ConsentBlock from '@/next/features/app-consent/consent-block';
import AppInspector from '@/next/features/app-views/inspector';
import { onOpenAppTab } from '@/next/features/app-views/open';
import { keptFrames } from '@/next/features/app-views/kept-frames';
import { parseAppTarget } from '@/next/features/app-views/target';
import type { IAppMenuItem } from '@/next/features/app-main/app-main';
import AppStage, { appStageDesign } from '@/next/features/app-main/app-stage';
import type { TChatOpeners } from '@/next/bindings/chat-view';
import { startFolder } from '@/next/bindings/home-composer';
import { useSiteStage } from '@/next/bindings/site-stage';
import { useChatApps } from '@/next/bindings/use-chat-apps';
import { setConversationApps } from '@/next/commands';
import { settled } from '@/next/bindings/outcome';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { useConversation, useConversationList, useConvsAnswered } from '@/next/stores/conversations';
import { useDefaultCwd } from '@/next/stores/projects';
import { useTabTitle } from '@/next/stores/tabs';
import { toast } from '@/next/ui/toast';
import { appBasePath } from '@/platform/config';
import { DOCS_APP, docsReaderPath } from '@/platform/router';
import { t } from '@/next/i18n';
import { useAppsDebug } from '@/next/features/app-views/debug-mode';

interface IAppMainPageProps {
  /** The machine of the screen; the chat's own machine wins when the address names a chat. */
  machine: MachineId;
  id: string;
  /** One of the app's objects open (`?o=`); null: the app's home. */
  object: string | null;
  chat: string | null;
  onOpenDrawer: () => void;
  /** Where a link, a file or a card of the chat opens (the pane beside the app). */
  open?: TChatOpeners;
  /** The right pane of the chat, as beside a design. */
  toolPane?: { open: boolean; onToggle: () => void } | null;
}

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

interface IAppStagePageProps extends Omit<IAppMainPageProps, 'machine'> {
  machine: MachineId;
  app: IInstalledApp;
  view: IAppView;
  target: 'app' | 'pane';
  viewPort: number;
  agentWalled: boolean;
}

/** The app in Sinopia's stage, once the app and its view are known. */
const AppStagePage = ({
  machine,
  id,
  object,
  chat,
  app,
  view,
  target,
  viewPort,
  agentWalled,
  onOpenDrawer,
  open,
  toolPane = null,
}: IAppStagePageProps) => {
  const [inspector, setInspector] = useState<'closed' | 'about' | 'debug'>('closed');
  const debugMode = useAppsDebug();
  const [reload, setReload] = useState(0);
  const chats = useConversationList(machine);
  const defaultCwd = useDefaultCwd(machine);
  const fromChat = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(fromChat);
  // The chats the app is on in are its own, as a design's chats are the design's.
  const tied = useMemo(
    () => (chats ?? []).filter((c) => c.apps?.includes(id)).map((c) => c.id as string),
    [chats, id],
  );
  const address = useMemo(
    () => ({ kind: ROUTE.app, id, ...(object ? { object } : {}) }) as const,
    [id, object],
  );
  const stage = useSiteStage({
    machine,
    design: appStageDesign(id, object),
    fromChat,
    dir: record?.cwd ?? startFolder(chats, machine, defaultCwd ?? null),
    tied,
    title: app.name,
    pageUrl: '',
    port: null,
    onOpenDrawer,
    open,
    keep: true,
    app: {
      address,
      onCreated: (made) => void settled(setConversationApps(machine, made, [id])).catch(() => undefined),
    },
  });
  const conv = stage.conv ?? null;
  const chatApps = useChatApps(machine, conv);
  useTabTitle(routePath({ kind: ROUTE.app, id, ...(object ? { object } : {}) }), object ?? app.name);

  // The app's own page is its one view: a pane view of it kept out of sight goes, rather than two players sounding.
  useEffect(() => keptFrames.dropApp(id), [id]);

  // A card's «Открыть в панели» of another app takes this page to that app; this one is already open. The chat goes
  // along only where that app is on.
  const chatOn = useConversation(conv)?.apps;
  useEffect(
    () =>
      onOpenAppTab((to) => {
        const next = parseAppTarget(to);
        if (next && next.app !== id)
          goTo({
            kind: ROUTE.app,
            id: next.app,
            ...(conv && chatOn?.includes(next.app) ? { chat: conv } : {}),
          });
      }),
    [id, conv, chatOn],
  );

  // The chat goes along to the object and back home: it is the one the person works in.
  const go = useCallback(
    (to: string | null) =>
      goTo({ kind: ROUTE.app, id, ...(to ? { object: to } : {}), ...(conv ? { chat: conv } : {}) }),
    [id, conv],
  );
  const { ask, fill } = stage;
  const on = conv !== null && (chatOn?.includes(id) ?? false);
  const handlers = useMemo(
    () => ({
      // The view's words go to the chat beside it where the app is on. Elsewhere they wait in the chat's field: the
      // person's own send makes the chat and turns the app on in it, never the view.
      chatSend: async (text: string) => {
        if (on) ask?.(fromApp(app.name, text));
        else fill?.(text);
      },
    }),
    [on, ask, fill, app.name],
  );

  const share = () => {
    // The page's own base (/m/<machine> on the hub) goes into the link, as the chat's «Скопировать ссылку» does.
    const link = `${window.location.origin}${appBasePath()}${routePath({ kind: ROUTE.app, id, ...(object ? { object } : {}) })}`;
    navigator.clipboard.writeText(link).then(
      () => toast(t('next.appmain.shared'), { description: t('next.appmain.sharedHint') }),
      () => toast(t('next.appmain.shareFailed'), { tone: 'error', description: link }),
    );
  };
  const menu: IAppMenuItem[] = [
    {
      id: 'about',
      icon: 'info',
      label: t('next.appmain.about'),
      onSelect: () => setInspector((now) => (now === 'about' ? 'closed' : 'about')),
    },
    {
      id: 'settings',
      icon: 'settings',
      label: t('next.appmain.settings'),
      onSelect: () => goTo({ kind: ROUTE.apps }),
    },
    ...(app.dev || debugMode
      ? [
          {
            id: 'debug',
            icon: 'bug',
            label: t('next.appmain.debug'),
            onSelect: () => setInspector((now) => (now === 'debug' ? 'closed' : 'debug')),
          },
        ]
      : []),
    ...(conv && chatApps.on.includes(app.id)
      ? [
          {
            id: 'off',
            icon: 'x',
            label: t('next.appmain.off'),
            danger: true,
            onSelect: () => void chatApps.turn(app.id, false),
          },
        ]
      : []),
    // The app sees this chat only where the person turned it on: the page offers it, it never takes it.
    ...(conv && !chatApps.on.includes(app.id)
      ? [
          {
            id: 'on',
            icon: 'plus',
            label: t('next.appmain.on'),
            onSelect: () => void chatApps.turn(app.id, true),
          },
        ]
      : []),
  ];

  return (
    <>
      <AppStage
        title={app.name}
        icon={app.icon || 'layout-grid'}
        object={object ? { title: object } : null}
        onHome={() => go(null)}
        stage={stage}
        menu={menu}
        onShare={share}
        toolPane={toolPane}
        onOpenDrawer={onOpenDrawer}
        frame={
          <ConsentStage machine={machine} app={app}>
            <AppFrame
              machine={machine}
              app={app}
              view={view}
              target={target}
              viewPort={viewPort}
              conversation={conv}
              reloadToken={reload}
              data={object ? { object } : undefined}
              handlers={handlers}
              onOpenObject={go}
            />
          </ConsentStage>
        }
        inspector={
          inspector === 'closed' ? null : (
            <AppInspector
              machine={machine}
              app={app}
              view={view}
              target={target}
              conversation={conv}
              debug={inspector === 'debug'}
              onReload={() => setReload((n) => n + 1)}
              agentWalled={agentWalled}
            />
          )
        }
      />
      <AppAsks machine={machine} conversation={conv} />
    </>
  );
};

const AppMainPage = ({
  machine: screen,
  id,
  object,
  chat,
  onOpenDrawer,
  open,
  toolPane,
}: IAppMainPageProps) => {
  const conv = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(conv);
  const answered = useConvsAnswered(record?.machineId ?? screen);
  const machine = record?.machineId ?? screen;
  const { list, error } = useInstalledApps(machine);

  const app = list?.apps.find((a) => a.id === id) ?? null;
  // A document's address came here from the reader (docs-redirect.ts); without the documents app the reader shows it.
  const toReader = list !== null && !app && id === DOCS_APP && object !== null;
  useEffect(() => {
    if (toReader && object !== null) window.location.replace(`${appBasePath()}${docsReaderPath(object)}`);
  }, [toReader, object]);
  // An app with no screen of its own opens its pane view here, so a press in the sidebar never ends on a blank page.
  const own = app?.views.find((v) => v.targets.includes('app'));
  const view = own ?? app?.views.find((v) => v.targets.includes('pane')) ?? null;

  if (error) return <p className="hx-appview__error">{error}</p>;
  // The chat named by the address is not known yet: the page waits for the list rather than showing the app alone and
  // then pulling a column in.
  if (list === null || (conv !== null && record === undefined && !answered))
    return <div className="hx-appview__wait" aria-busy="true" />;
  if (toReader) return <div className="hx-appview__wait" aria-busy="true" />;
  if (!app) return <p className="hx-appview__error">{t('next.appmain.gone')}</p>;
  // An app with a window that waits for consent shows the bar over it (ConsentStage); one with no window says
  // what it waits for here.
  if (!view && app.consent) return <ConsentBlock className="hx-consentblock--page" app={app} />;
  if (!view) return <p className="hx-appview__error">{t('next.appmain.noScreen')}</p>;

  return (
    <AppStagePage
      machine={machine}
      id={id}
      object={object}
      chat={chat}
      app={app}
      view={view}
      target={own ? 'app' : 'pane'}
      viewPort={list.viewPort}
      agentWalled={list.agentWalled}
      onOpenDrawer={onOpenDrawer}
      open={open}
      toolPane={toolPane}
    />
  );
};

export default AppMainPage;
