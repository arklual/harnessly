// An app opened as the main thing, /apps/<id>?chat=<conv>: its `app` view fills the page, the chat it works in stands
// beside it (a column, a window or its field at the foot — the viewer's choice, kept), and the page's «…» holds the
// app's rights, the debug section of a dev install and turning the app off in this chat. Without a chat the app stands
// alone.
import { useCallback, useEffect, useRef, useState } from 'react';
import AppAsks from '@/next/features/app-views/asks';
import AppFrame from '@/next/features/app-views/app-frame';
import { useInstalledApps } from '@/next/features/app-views/api';
import AppInspector from '@/next/features/app-views/inspector';
import { onOpenAppTab } from '@/next/features/app-views/open';
import { switchedByView } from '@/next/features/app-views/chat-handoff';
import { parseAppTarget } from '@/next/features/app-views/target';
import AppIsland from '@/next/features/app-main/app-island';
import AppMain, { type IAppMenuItem } from '@/next/features/app-main/app-main';
import {
  keepLayout,
  PLACEMENT,
  readLayout,
  withPlacement,
  type IAppLayout,
} from '@/next/features/app-main/placement';
import { lastLineOf } from '@/next/features/designly/last-line';
import ChatView from '@/next/bindings/chat-view';
import { useFeedItems } from '@/next/bindings/feed-items';
import { useChatApps } from '@/next/bindings/use-chat-apps';
import { interrupt, sendMessage } from '@/next/commands';
import { settled } from '@/next/bindings/outcome';
import { hub, useConversationChannel } from '@/next/core/realtime/hub';
import { useCentralFeed } from '@/next/bindings/use-central';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { useConversation, useConvsAnswered } from '@/next/stores/conversations';
import { useTabTitle } from '@/next/stores/tabs';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

interface IAppMainPageProps {
  /** The machine of the screen; the chat's own machine wins when the address names a chat. */
  machine: MachineId;
  id: string;
  chat: string | null;
  onOpenDrawer: () => void;
}

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

/** The field at the foot, wired to the chat: the agent's last line, whether it works, and the words it sends. */
const ChatIsland = ({
  machine,
  conv,
  onExpand,
}: {
  machine: MachineId;
  conv: ConvId;
  onExpand: () => void;
}) => {
  // The chat's own view is not mounted under the folded field: its feed is read from here.
  useConversationChannel(machine, conv);
  useCentralFeed(machine, conv);
  const items = useFeedItems(conv, machine);
  const state = useConversation(conv)?.state.k;
  let last: string | null = null;
  for (let i = (items?.length ?? 0) - 1; i >= 0; i -= 1) {
    const item = items?.[i];
    if (item?.kind === 'turn' && item.answer) {
      last = lastLineOf(item.answer.md);
      break;
    }
  }
  return (
    <AppIsland
      last={last}
      busy={state === 'starting' || state === 'working'}
      onSend={(text) => sendMessage(machine, { conversationId: conv, text })}
      onStop={() =>
        void settled(interrupt(machine, conv)).catch((e: Error) =>
          toast(t('next.appmain.stopFailed'), { tone: 'error', description: e.message }),
        )
      }
      onExpand={onExpand}
    />
  );
};

const AppMainPage = ({ machine: screen, id, chat, onOpenDrawer }: IAppMainPageProps) => {
  const conv = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(conv);
  const answered = useConvsAnswered(record?.machineId ?? screen);
  const machine = record?.machineId ?? screen;
  const { list, error } = useInstalledApps(machine);
  const [layout, setLayout] = useState<IAppLayout>(readLayout);
  const [inspector, setInspector] = useState<'closed' | 'about' | 'debug'>('closed');
  const [reload, setReload] = useState(0);
  const chatApps = useChatApps(machine, conv);
  // The chat the view's session is for. It follows the page's chat, except to a chat the view itself asked for: then
  // the view stays as it is, with no new session and no reload, and is only told the chat beside it (shownChat).
  const shownConv = record !== undefined && conv !== null ? conv : null;
  const sessionConv = useRef<ConvId | null>(shownConv);
  if (shownConv !== sessionConv.current && !switchedByView(id, shownConv)) sessionConv.current = shownConv;

  const app = list?.apps.find((a) => a.id === id) ?? null;
  const view = app?.views.find((v) => v.targets.includes('app')) ?? null;
  useTabTitle(routePath({ kind: ROUTE.app, id, ...(chat ? { chat } : {}) }), app?.name ?? null);

  // A card's «Открыть в панели» of another app takes this page to that app; this one is already open. The chat goes
  // along only where that app is on.
  const chatOn = record?.apps;
  useEffect(
    () =>
      onOpenAppTab((target) => {
        const to = parseAppTarget(target);
        if (to && to.app !== id)
          goTo({ kind: ROUTE.app, id: to.app, ...(chat && chatOn?.includes(to.app) ? { chat } : {}) });
      }),
    [id, chat, chatOn],
  );

  const place = useCallback((next: IAppLayout) => {
    setLayout(next);
    keepLayout(next);
  }, []);

  if (error) return <p className="hx-appview__error">{error}</p>;
  // The chat named by the address is not known yet: the page waits for the list rather than showing the app alone and
  // then pulling a column in.
  if (list === null || (conv !== null && record === undefined && !answered))
    return <div className="hx-appview__wait" aria-busy="true" />;
  if (!app || !view) return <p className="hx-appview__error">{t('next.appmain.gone')}</p>;

  const onChat = record !== undefined && conv !== null;
  const back = onChat ? () => goTo({ kind: ROUTE.chat, id: conv }) : null;
  const menu: IAppMenuItem[] = [
    {
      id: 'about',
      icon: 'info',
      label: t('next.appmain.about'),
      onSelect: () => setInspector((now) => (now === 'about' ? 'closed' : 'about')),
    },
    ...(app.dev
      ? [
          {
            id: 'debug',
            icon: 'bug',
            label: t('next.appmain.debug'),
            onSelect: () => setInspector((now) => (now === 'debug' ? 'closed' : 'debug')),
          },
        ]
      : []),
    ...(onChat && chatApps.on.includes(app.id)
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
    ...(onChat && !chatApps.on.includes(app.id)
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
      <AppMain
        title={app.name}
        chatTitle={onChat ? (record?.title ?? null) : null}
        placement={layout.placement}
        float={layout.float}
        onPlacement={(placement, section) => place(withPlacement(layout, placement, section))}
        onFloat={(float) => place({ ...layout, float })}
        onBack={back}
        onOpenDrawer={onOpenDrawer}
        menu={menu}
        stage={
          <AppFrame
            machine={machine}
            app={app}
            view={view}
            target="app"
            viewPort={list.viewPort}
            conversation={sessionConv.current}
            shownChat={onChat ? conv : null}
            reloadToken={reload}
          />
        }
        chat={
          onChat ? (
            <ChatView
              key={conv}
              conv={conv}
              machine={machine}
              onRetryLoad={() => hub.client(machine)?.reconnectNow()}
              onShowBackground={() => goTo({ kind: ROUTE.chat, id: conv })}
            />
          ) : null
        }
        island={
          onChat ? (
            <ChatIsland
              machine={machine}
              conv={conv}
              onExpand={() => place({ ...layout, placement: PLACEMENT.left })}
            />
          ) : null
        }
        inspector={
          inspector === 'closed' ? null : (
            <AppInspector
              machine={machine}
              app={app}
              view={view}
              target="app"
              conversation={onChat ? conv : null}
              debug={inspector === 'debug'}
              onReload={() => setReload((n) => n + 1)}
              agentWalled={list.agentWalled}
            />
          )
        }
      />
      <AppAsks machine={machine} conversation={onChat ? conv : null} />
    </>
  );
};

export default AppMainPage;
