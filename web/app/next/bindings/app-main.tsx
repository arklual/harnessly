// An app opened as the main thing, /apps/<id>?chat=<conv>: its `app` view fills the page, the chat it works in stands
// beside it (a column, a window or its field at the foot — the viewer's choice, kept), and the page's «…» holds the
// app's rights, the debug section of a dev install and turning the app off in this chat. Without a chat the app stands
// alone.
import { useCallback, useEffect, useState } from 'react';
import AppAsks from '@/next/features/app-views/asks';
import AppFrame from '@/next/features/app-views/app-frame';
import { ConsentStage } from '@/next/features/app-consent/consent-ask';
import { useInstalledApps } from '@/next/features/app-views/api';
import ConsentBlock from '@/next/features/app-consent/consent-block';
import AppInspector from '@/next/features/app-views/inspector';
import { onOpenAppTab } from '@/next/features/app-views/open';
import { switchedByView } from '@/next/features/app-views/chat-handoff';
import { keptFrames } from '@/next/features/app-views/kept-frames';
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
import { createConversation, interrupt, sendMessage, setConversationApps } from '@/next/commands';
import { settled } from '@/next/bindings/outcome';
import { hub, useConversationChannel } from '@/next/core/realtime/hub';
import { useCentralFeed } from '@/next/bindings/use-central';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, routePath } from '@/next/routes/routes';
import { useConversation, useConversationList, useConvsAnswered } from '@/next/stores/conversations';
import { ChatSwitch } from '@/next/features/designly/chat-switch';
import { chatStatusOf } from '@/next/features/sidebar/status';
import { useConnection } from '@/next/stores/connection';
import { useTabTitle } from '@/next/stores/tabs';
import { toast } from '@/next/ui/toast';
import { appBasePath } from '@/platform/config';
import { t } from '@/next/i18n';
import { useAppsDebug } from '@/next/features/app-views/debug-mode';
import { useAgentLimits } from '@/next/bindings/use-limits';
import LimitsChip from '@/next/features/limits/limits-chip';
import { limitsProviderOf } from '@/next/features/limits/model';

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
  const record = useConversation(conv);
  const state = record?.state.k;
  // The same limits as the full field's chip: the subscription's windows and their card.
  const limits = useAgentLimits(
    machine,
    limitsProviderOf(record ? { provider: record.provider, profile: record.model?.profile } : null),
  );
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
      limits={limits && <LimitsChip view={limits} />}
    />
  );
};

const AppMainPage = ({ machine: screen, id, chat }: IAppMainPageProps) => {
  const conv = chat !== null && isConvId(chat) ? chat : null;
  const record = useConversation(conv);
  const answered = useConvsAnswered(record?.machineId ?? screen);
  const machine = record?.machineId ?? screen;
  const { list, error } = useInstalledApps(machine);
  const [layout, setLayout] = useState<IAppLayout>(readLayout);
  const [inspector, setInspector] = useState<'closed' | 'about' | 'debug'>('closed');
  const debugMode = useAppsDebug();
  const [reload, setReload] = useState(0);
  const chatApps = useChatApps(machine, conv);
  const chats = useConversationList(machine);
  const connection = useConnection(machine);
  const [making, setMaking] = useState(false);
  // The chat the view's session is for. It follows the page's chat, except to a chat the view itself asked for: then
  // the view stays as it is, with no new session and no reload, and is only told the chat beside it (shownChat).
  const shownConv = record !== undefined && conv !== null ? conv : null;
  const [sessionConv, setSessionConv] = useState<ConvId | null>(shownConv);
  if (shownConv !== sessionConv && !switchedByView(id, shownConv)) setSessionConv(shownConv);

  const app = list?.apps.find((a) => a.id === id) ?? null;
  // An app with no screen of its own opens its pane view here, so a press in the sidebar never ends on a blank page.
  const own = app?.views.find((v) => v.targets.includes('app'));
  const view = own ?? app?.views.find((v) => v.targets.includes('pane')) ?? null;
  const target = own ? 'app' : 'pane';
  useTabTitle(routePath({ kind: ROUTE.app, id, ...(chat ? { chat } : {}) }), app?.name ?? null);

  // The app's own page is its one view: a pane view of it kept out of sight goes, rather than two players sounding.
  useEffect(() => keptFrames.dropApp(id), [id]);

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
  if (!app) return <p className="hx-appview__error">{t('next.appmain.gone')}</p>;
  // An app with a window that waits for consent shows the bar over it (ConsentStage); one with no window says
  // what it waits for here.
  if (!view && app.consent) return <ConsentBlock className="hx-consentblock--page" app={app} />;
  if (!view) return <p className="hx-appview__error">{t('next.appmain.noScreen')}</p>;

  const onChat = record !== undefined && conv !== null;
  const open = (to: ConvId | null) => goTo({ kind: ROUTE.app, id, ...(to ? { chat: to } : {}) });
  // The switch lists the chats of this machine the app is on in, and the chat on screen even where it is off.
  const online = connection === undefined || connection.k === 'live';
  const rows = (chats ?? [])
    .filter((c) => c.id === conv || c.apps?.includes(id))
    .map((c) => ({
      id: c.id,
      title: c.title,
      machine: null,
      status: chatStatusOf(c, online),
      at: c.lastActivityAt,
    }));
  // A new chat starts where the chat on screen works (or the latest one of the machine), with the app already on in it.
  const like = record ?? chats?.[0];
  const newChat = like
    ? async () => {
        if (making) return;
        setMaking(true);
        try {
          const made = await createConversation(machine, {
            where: like.projectId ? { projectId: like.projectId } : { cwd: like.cwd },
            provider: like.provider,
            model: like.model,
          }).conversation;
          if (made === null) throw new Error(t('next.appmain.newChatFailed'));
          await settled(setConversationApps(machine, made, [id]));
          open(made);
        } catch (e) {
          toast(t('next.appmain.newChatFailed'), {
            tone: 'error',
            description: e instanceof Error ? e.message : undefined,
          });
        } finally {
          setMaking(false);
        }
      }
    : null;
  const chatSwitch = (
    <ChatSwitch
      current={onChat ? conv : null}
      title={onChat ? (record?.title ?? '') : t('next.appmain.noChat')}
      rows={rows}
      onPick={(to) => open(to as ConvId | null)}
      chatsLabel={t('next.appmain.chats')}
      lead={[
        ...(newChat
          ? [
              {
                id: 'new',
                icon: 'square-pen',
                label: t('next.appmain.newChat'),
                selected: false,
                onPick: () => void newChat(),
              },
            ]
          : []),
        {
          id: 'none',
          icon: 'app-window',
          label: t('next.appmain.noChat'),
          selected: !onChat,
          onPick: () => open(null),
        },
      ]}
    />
  );
  const share = () => {
    // The page's own base (/m/<machine> on the hub) goes into the link, as the chat's «Скопировать ссылку» does.
    const link = `${window.location.origin}${appBasePath()}${routePath({ kind: ROUTE.app, id })}`;
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
        icon={app.icon || 'layout-grid'}
        chatTitle={onChat ? (record?.title ?? null) : null}
        chatSwitch={chatSwitch}
        onShare={share}
        placement={layout.placement}
        float={layout.float}
        onPlacement={(placement, section) => place(withPlacement(layout, placement, section))}
        onFloat={(float) => place({ ...layout, float })}
        menu={menu}
        stage={
          <ConsentStage machine={machine} app={app}>
            <AppFrame
              machine={machine}
              app={app}
              view={view}
              target={target}
              viewPort={list.viewPort}
              conversation={sessionConv}
              shownChat={onChat ? conv : null}
              reloadToken={reload}
            />
          </ConsentStage>
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
              target={target}
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
