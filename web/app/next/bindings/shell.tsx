// The new interface's shell on the v2 stores: the frame, the sidebar, the header and the open chat. The
// components take props; the models in this folder turn the stores into them. A conversation is drawn from the
// moment its id is in the address — its feed is the store's, whatever the sockets do.
import '@/next/styles/tokens.css';
import '@/next/styles/motion.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';
import '@/next/styles/sidebar.css';
import '@/next/styles/header.css';
import '@/next/styles/tabs.css';

import { useCallback, useEffect, useMemo, useState, type ComponentProps } from 'react';
import { appConfig } from '@/platform/config';
import { useTheme } from '@/platform/theme';
import { useAgentTerminal } from '@/next/bindings/agent-terminal';
import { useRightPane } from '@/next/bindings/right-pane';
import { useChatBackground } from '@/next/bindings/chat-background';
import { useCentralHistory } from '@/next/bindings/use-central';
import ChatView from '@/next/bindings/chat-view';
import HomeComposer from '@/next/bindings/home-composer';
import { machineSwitchLeavesChat, useShellModel, useTabChats } from '@/next/bindings/shell-model';
import { bootNext, shutdownNext, useBoot } from '@/next/boot';
import { markSeen, setGoal } from '@/next/commands';
import { connectionStore } from '@/next/stores/connection';
import { useAll } from '@/next/core/store';
import { hub } from '@/next/core/realtime/hub';
import ChatHeader from '@/next/features/header/chat-header';
import type { IHeaderGoal } from '@/next/features/header/goal-dialog';
import { settled } from '@/next/bindings/outcome';
import { BackgroundButton, TerminalButton } from '@/next/features/header/header-buttons';
import { notificationsFromChats } from '@/next/features/notifications/from-chats';
import { notificationsFromMachines } from '@/next/features/notifications/from-machines';
import NotificationCenter from '@/next/features/notifications/notification-center';
import NoticeStack from '@/next/features/notifications/notice-stack';
import { SecretAskHost } from '@/next/bindings/safe-asks';
import { useNotices } from '@/next/bindings/notices';
import { CHAT_SCOPES, messageGroups, recentChats, searchChats } from '@/next/features/search/search-chats';
import { useMessageSearch } from '@/next/bindings/use-message-search';
import { revealMessage } from '@/next/stores/reveal';
import type { ISearchRow } from '@/next/features/search/search-dialog';
import AppFrame, { type IAppFrameProps } from '@/next/features/shell/app-frame';
import { toggleRail, useRail } from '@/next/bindings/rail';
import ChatStage from '@/next/features/shell/chat-stage';
import QuietChat from '@/next/bindings/quiet-chat';
import { PHONE_QUERY } from '@/next/features/shell/gestures';
import Greeting from '@/next/features/shell/greeting';
import HomeTools, { type IHomeTool } from '@/next/features/shell/home-tools';
import { PeopleBlock } from '@/next/bindings/asks';
import RailChats from '@/next/features/sidebar/rail-chats';
import { useShellTabs, wantsNewTab } from '@/next/bindings/tabs';
import { useTopTabs } from '@/next/stores/tabs';
import { usePhone } from '@/next/ui/use-media';
import Sidebar from '@/next/features/sidebar/sidebar';
import LegacyScreen, { LEGACY_SCREEN } from '@/next/legacy/screens';
import {
  SIDEBAR_VIEW,
  type IMoreItem,
  type ISidebarChat,
  type ISidebarProject,
} from '@/next/features/sidebar/types';
import Icon from '@/next/ui/icon';
import LazyView from '@/next/ui/lazy-view';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { goTo, HOME, ROUTE, useRoute, type TRoute } from '@/next/routes/routes';
import AppsScreen from '@/next/bindings/apps';
import SettingsScreen from '@/next/bindings/settings';
import DesignlyView from '@/next/bindings/designly';
import DeckCreateView from '@/next/bindings/designly-deck-new';
import DesignlySite from '@/next/bindings/designly-site';
import SiteWait from '@/next/features/designly/site-wait';
import InviteAccept from '@/next/features/designly/access/invite-accept';
import { useProjectDialog, useResumeDialog } from '@/next/bindings/shell-dialogs';
import { useAppUpdate } from '@/next/bindings/use-app-update';
import { useRelease } from '@/next/bindings/use-release';
import ReleaseNotice from '@/next/features/update/release-notice';
import UpdateBanner from '@/next/features/update/update-banner';
import WhatsNew from '@/next/features/whats-new/whats-new';
import { REFS_MAX, type ConvId } from '@/next/protocol/v2';
import { chatRef } from '@/next/features/composer/chat-sources';
import { addDraftRef } from '@/next/stores/drafts';
import { isUnread, useConversation, useConvsAnswered } from '@/next/stores/conversations';
import { linkReason, useConnection } from '@/next/stores/connection';
import { useMe } from '@/next/stores/me';
import { useHomeMachine } from '@/next/stores/machines';
import { useCentral } from '@/next/stores/central';
import { useFeed } from '@/next/stores/conversation';
import { newChatProjectStore, useNewChatProject } from '@/next/stores/new-chat';
import { UI_REQUEST, useUiRequest } from '@/next/stores/ui-requests';
import { ToastHost, toast } from '@/next/ui/toast';
import { useDeadline } from '@/next/ui/use-deadline';
import { FLY_FIELD, flyComposer } from '@/next/ui/fly-composer';

const CHAT_LEAVE_MS = 180;
const NO_CHATS: readonly ISidebarChat[] = [];
const NO_PROJECTS: readonly ISidebarProject[] = [];

/** The value as the stage shows it: a change to another chat waits `ms` while the shown one leaves; to or from no
 *  chat it follows at once. */
const useLeaving = (value: string | null, ms: number): string | null => {
  const [shown, setShown] = useState(value);
  const swap = value !== null && shown !== null && value !== shown;
  if (!swap && shown !== value) setShown(value);
  useEffect(() => {
    if (!swap) return;
    const timer = setTimeout(() => setShown(value), ms);
    return () => clearTimeout(timer);
  }, [swap, value, ms]);
  return shown;
};
/** Longer than the search dialog's slowest exit (styles/shell.css: the scrim 180 ms, the box 160 ms). */
const SEARCH_EXIT_MS = 260;
const loadSearchDialog = () => import('@/next/features/search/search-dialog');

/** A chat named by the address that the list does not have yet (a tab started a second ago) is waited for this long. */
const CHAT_WAIT_MS = 15_000;
/** The day groups and «5 мин» move on with this clock. */
const CLOCK_MS = 60_000;

/** The frame with the rail read here, so that folding the sidebar draws the frame and not the shell around it. */
const RailFrame = (props: Omit<IAppFrameProps, 'rail'>) => <AppFrame {...props} rail={useRail()} />;

/** The Designly site's stage, which gives its room back to the page when the sidebar is folded. */
const RailSite = (props: Omit<ComponentProps<typeof DesignlySite>, 'maxed'>) => (
  <DesignlySite {...props} maxed={useRail()} />
);

/** The cluster's copy of the chats follows the links: asked again when one goes down or comes back. */
const useCentralList = () => {
  const links = useAll(connectionStore, (c) =>
    [...c.keys()]
      .map((m) => `${m}:${c.get(m)?.k}`)
      .sort()
      .join(','),
  );
  useCentralHistory(links);
};

const useClock = () => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), CLOCK_MS);
    return () => clearInterval(timer);
  }, []);
  return now;
};

/** `/?c=<legacy key>` of the current interface: `agent:<machine>:<tab>` or `agent:<tab>`; the tab is the chat's id. */
const legacyChatId = (search: string): string | null => {
  const key = new URLSearchParams(search).get('c');
  const tab = key?.split(':').pop();
  return tab?.startsWith('tab-') ? tab : null;
};

const isConvId = (id: string): id is ConvId => id.startsWith('tab-');

/** Which of the sidebar's screens is on: Designly and its designs, Settings, or the chats. */
const sidebarView = (route: TRoute) => {
  if (
    route.kind === ROUTE.designly ||
    route.kind === ROUTE.designlyNew ||
    route.kind === ROUTE.site ||
    route.kind === ROUTE.invite
  )
    return SIDEBAR_VIEW.designly;
  return route.kind === ROUTE.settings ? SIDEBAR_VIEW.settings : SIDEBAR_VIEW.chat;
};

interface INotificationsState {
  anchor: HTMLElement;
  side: 'right' | 'bottom';
}

export interface IShellProps {
  owner: string;
}

/** The interface cannot start: said the way the start page speaks, with the one thing to do under it. */
const BootNotice = ({ kind }: { kind: 'failed' | 'no-v2' }) => (
  <div className="hx-stage" data-phase="home">
    <div className="hx-hello">
      <div className="hx-enter" data-enter="up" data-eb="420" role="status">
        <div className="hx-hello__title">
          {kind === 'failed' ? t('next.boot.failed') : t('next.boot.noV2')}
        </div>
        <div className="hx-hello__sub">
          {kind === 'failed' ? t('next.boot.failedNote') : t('next.boot.noV2Note')}
        </div>
        {kind === 'failed' && (
          <button
            type="button"
            className="hx-hello__resume"
            onClick={() => void shutdownNext().then(() => bootNext())}
          >
            <Icon name="rotate-ccw" size="sm" />
            {t('next.boot.retry')}
          </button>
        )}
      </div>
    </div>
  </div>
);

const Shell = ({ owner }: IShellProps) => {
  const model = useShellModel();
  const boot = useBoot();
  const route = useRoute();
  const me = useMe();
  const now = useClock();
  useCentralList();
  const update = useAppUpdate();
  const release = useRelease(model.active);
  const currentId = route.kind === ROUTE.chat ? route.id : null;
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [notifications, setNotifications] = useState<INotificationsState | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  // The dialog stays mounted through its exit (the scrim and the box fade out in CSS), then goes.
  const search = usePresentFlag(searchOpen, SEARCH_EXIT_MS);
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState(CHAT_SCOPES[0]?.id ?? 'all');
  const login = owner.split('@')[0] ?? '';
  // The tabs over the window: a browser's, on a wide window, unless switched off in Settings.
  const phone = usePhone();
  const tabs = useShellTabs(useTabChats(), useTopTabs() && !phone);
  const name = (me.k === 'ready' ? me.me.name : null) ?? appConfig().people[login] ?? null;

  // While the profile loads there is no name to show: the footer draws a placeholder, not the login.
  const loginName = me.k === 'loading' ? null : login;
  const personName = name ?? loginName;
  const person = personName === null ? null : { name: personName };

  const { navigate, openInNewTab } = tabs;
  const openChat = useCallback(
    (id: string) => {
      navigate({ kind: ROUTE.chat, id });
      setDrawerOpen(false);
    },
    [navigate],
  );

  // An address of the current interface (`/?c=<key>`) opens the same chat here, once, on load.
  useEffect(() => {
    const id = legacyChatId(window.location.search);
    if (id) goTo({ kind: ROUTE.chat, id }, { replace: true });
  }, []);

  // Switching chats, the open one leaves first (its thread fades up, 180 ms), then the next one comes in.
  const stageId = useLeaving(currentId, CHAT_LEAVE_MS);
  const leaving = stageId !== currentId;
  // A chat that opens from the start page: its field comes down from under the greeting, as in the design.
  const [arrival, setArrival] = useState<{ id: string | null; fromHome: boolean }>({
    id: stageId,
    fromHome: false,
  });
  if (arrival.id !== stageId) setArrival({ id: stageId, fromHome: arrival.id === null });
  // The start page's field rises from the chat's place only when a chat was the screen before it.
  const [screens, setScreens] = useState<{ now: TRoute['kind']; before: TRoute['kind'] | null }>({
    now: route.kind,
    before: null,
  });
  if (screens.now !== route.kind) setScreens({ now: route.kind, before: screens.now });
  const record = useConversation(stageId && isConvId(stageId) ? stageId : null);
  const feed = useFeed(stageId && isConvId(stageId) ? stageId : null);
  const chatMachine = record?.machineId ?? feed?.machineId ?? null;
  const term = useAgentTerminal(route.kind === ROUTE.chat ? (record ?? null) : null);
  const bg = useChatBackground(stageId && isConvId(stageId) ? stageId : null, chatMachine, now);
  const present = stageId !== null && chatMachine !== null;

  // The machine of the open chat holds the open conversation's channel. With the one list of every machine,
  // the start page is the machine the person works on again, so a new chat starts there and not on the last one opened.
  const home = useHomeMachine();
  const oneList = useCentral().enabled;
  useEffect(() => {
    if (chatMachine) hub.setActive(chatMachine);
    else if (route.kind === ROUTE.home && oneList && home) hub.setActive(home);
  }, [chatMachine, route.kind, oneList, home]);

  // A machine picked in the sidebar while a chat of another one is on screen: the screen goes to the picked machine's
  // start page (its tab, or a new one), so the list, the machine card and the screen are of one machine. Else the open
  // chat would stay with the other machine's list beside it, and the next switch to it would swap the whole list back.
  const { pickMachine: pickActive } = model;
  const pickMachine = useCallback(
    async (id: string) => {
      await pickActive(id);
      if (machineSwitchLeavesChat(chatMachine, id)) navigate({ kind: ROUTE.home });
    },
    [pickActive, chatMachine, navigate],
  );

  // A chat the list does not have: wait for it, then say it is not there instead of a skeleton for ever.
  // (the list the browser kept draws at once, but only the machine's own answer says a chat is not in it)
  const listed = useConvsAnswered(model.active);
  const [missing, setMissing] = useState<string | null>(null);
  useEffect(() => {
    if (!currentId || present || !listed) return;
    const timer = setTimeout(() => setMissing(currentId), CHAT_WAIT_MS);
    return () => clearTimeout(timer);
  }, [currentId, present, listed]);

  // Reading the chat reads it: what the agent finished while it is on screen is seen.
  const unreadUpTo = record && isUnread(record) ? (record.lastTurn?.endedAt ?? null) : null;
  useEffect(() => {
    if (!record || unreadUpTo === null || document.visibilityState !== 'visible') return;
    void markSeen(record.machineId, record.id, unreadUpTo);
  }, [record, unreadUpTo]);

  // Back on a wide window, the phone drawer's state would leave the window inert: it closes.
  useEffect(() => {
    const phone = window.matchMedia(PHONE_QUERY);
    const onChange = () => {
      if (!phone.matches) setDrawerOpen(false);
    };
    phone.addEventListener('change', onChange);
    return () => phone.removeEventListener('change', onChange);
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const mod = (event.metaKey || event.ctrlKey) && !event.altKey;
      if (mod && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setSearchOpen((current) => !current);
      } else if (mod && event.key.toLowerCase() === 'b') {
        event.preventDefault();
        toggleRail();
      } else if (event.key === 'Escape' && drawerOpen) setDrawerOpen(false);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [drawerOpen]);

  const notices = useNotices({ person: owner || 'local', currentId, onOpenChat: openChat });

  const chat = useMemo(
    () => (currentId === null ? null : (model.chats?.find((item) => item.id === currentId) ?? null)),
    [currentId, model.chats],
  );
  const parentChat = useMemo(
    () => (chat?.parentId ? (model.chats?.find((item) => item.id === chat.parentId) ?? null) : null),
    [chat, model.chats],
  );
  const notificationItems = useMemo(() => {
    const fromChats = notificationsFromChats(model.chats);
    return fromChats && [...notificationsFromMachines(model.machineNotices), ...fromChats];
  }, [model.chats, model.machineNotices]);
  const [machineReveal, setMachineReveal] = useState(0);
  const unread = notificationItems?.some((item) => item.unread) ?? false;
  const messages = useMessageSearch(model.active, query, searchOpen);
  const searchResults = useMemo(() => {
    if (model.chats === null) return null;
    const titles = searchChats(model.chats, model.projects ?? [], query);
    // The messages are still being asked and no title answers yet: the dialog keeps its «searching» rather than «nothing».
    if (messages.asked && messages.hits === null && titles.length === 0) return null;
    return messages.hits ? [...titles, ...messageGroups(messages.hits, model.chats, now)] : titles;
  }, [model.chats, model.projects, query, messages.asked, messages.hits, now]);
  const recent = useMemo(
    () => (model.chats === null ? [] : recentChats(model.chats, model.projects ?? [])),
    [model.chats, model.projects],
  );

  const toggleNotifications = (anchor: HTMLElement, side: INotificationsState['side']) => {
    // On the phone the list is a sheet over the page: the side menu it was asked from lets go of the page first.
    if (notifications === null && phone) setDrawerOpen(false);
    setNotifications((current) => (current ? null : { anchor, side }));
  };

  // «More» of the design: its screens, opened as pages of the new interface; on the phone the drawer lets go of the page.
  const openScreen = (next: TRoute) => {
    navigate(next);
    setDrawerOpen(false);
  };
  const { resolvedTheme, setTheme } = useTheme();
  const profile: readonly IMoreItem[] = [
    {
      id: 'settings',
      icon: 'settings',
      label: t('next.shell.settings'),
      current: route.kind === ROUTE.settings,
      onSelect: () => openScreen({ kind: ROUTE.settings, section: null }),
    },
  ];
  const more: readonly IMoreItem[] = [
    {
      id: 'apps',
      icon: 'app-window',
      label: t('next.sidebar.apps'),
      current: route.kind === ROUTE.apps,
      onSelect: () => openScreen({ kind: ROUTE.apps }),
    },
    {
      id: 'efficiency',
      icon: 'gauge',
      label: t('next.sidebar.efficiency'),
      current: route.kind === ROUTE.efficiency,
      onSelect: () => openScreen({ kind: ROUTE.efficiency }),
    },
    {
      id: 'stats',
      icon: 'chart-column',
      label: t('next.sidebar.stats'),
      current: route.kind === ROUTE.stats,
      onSelect: () => openScreen({ kind: ROUTE.stats }),
    },
  ];

  // No machine runs the new runtime: nothing will load, the list is empty rather than a skeleton forever.
  const noV2 = boot.k === 'ready' && boot.machines.length === 0;
  const machineName = model.machine?.name ?? '';
  // A list that does not come says so, with a retry, instead of a skeleton for ever; the chats already listed stay
  // whatever the link does.
  const connection = useConnection(model.active);
  const chatsWait = useDeadline(model.chats === null && boot.k === 'ready' && !noV2);
  const retryChats = () => {
    if (boot.k === 'failed') return shutdownNext().then(() => bootNext());
    if (model.active) hub.reconnectNow(model.active);
    chatsWait.restart();
  };
  let listFailed: { reason: string; onRetry: () => void | Promise<unknown> } | null = null;
  if (model.chats === null && boot.k === 'failed')
    listFailed = { reason: t(`next.failure.${boot.code}`), onRetry: retryChats };
  else if (model.chats === null && chatsWait.expired)
    listFailed = { reason: linkReason(connection) ?? t('next.load.noAnswer'), onRetry: retryChats };
  // The header's buttons of the right stand in the pane's strip while it is open.
  const paneStrip =
    term.header || bg.header ? (
      <>
        {term.header && <TerminalButton terminal={term.header} />}
        {bg.header && <BackgroundButton background={bg.header} />}
      </>
    ) : null;
  const pane = useRightPane(
    route.kind === ROUTE.chat ? (record ?? null) : null,
    machineName,
    term.reveal,
    paneStrip,
  );
  const projectDialog = useProjectDialog(model.active);
  const resumeDialog = useResumeDialog(model.active, machineName, now, (id) =>
    navigate({ kind: ROUTE.chat, id }),
  );
  // «/resume» in the field opens the same list of earlier sessions as the start page's link.
  useUiRequest(UI_REQUEST.resume, resumeDialog.show);
  const newChatProject = useNewChatProject();
  // A project picked for the next chat that the list no longer has is no pick.
  const newChatInto = model.projects?.find((project) => project.id === newChatProject) ?? null;
  const newChatName = newChatInto?.name ?? null;
  const into = useMemo(
    () => (newChatName === null ? null : { name: newChatName, onClear: () => newChatProjectStore.set(null) }),
    [newChatName],
  );
  const chatActions = {
    ...model.actions,
    onOpenInNewTab: tabs.enabled ? (id: string) => openInNewTab({ kind: ROUTE.chat, id }, true) : undefined,
    onNewProject: projectDialog.show,
    onAttachChat:
      currentId !== null && isConvId(currentId)
        ? (id: string) => {
            const chat = model.chats?.find((item) => item.id === id);
            if (chat && id !== currentId)
              addDraftRef(currentId, chatRef(chat.id as ConvId, chat.title), REFS_MAX);
          }
        : undefined,
    onNewChatIn: (projectId: string) => {
      newChatProjectStore.set(projectId);
      navigate(HOME);
      setDrawerOpen(false);
    },
  };
  const sidebar = (
    <Sidebar
      brand={appConfig().productName}
      brandHref={appConfig().hubUrl ? `${appConfig().hubUrl}/` : null}
      chats={noV2 ? NO_CHATS : model.chats}
      projects={noV2 ? NO_PROJECTS : model.projects}
      favorites={model.favorites}
      loadError={listFailed?.reason ?? null}
      onRetryLoad={listFailed?.onRetry}
      currentId={currentId}
      view={sidebarView(route)}
      now={now}
      person={person}
      machine={model.machine}
      otherMachines={model.otherMachines}
      machineReveal={machineReveal}
      unread={unread}
      notificationsOpen={notifications !== null}
      searchOpen={searchOpen}
      more={more}
      profile={profile}
      theme={{
        dark: resolvedTheme === 'dark',
        onToggle: () => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark'),
      }}
      actions={chatActions}
      onOpen={openChat}
      onNewChat={() => {
        newChatProjectStore.set(null);
        // From Designly the field flies back down to the start page's.
        if (!tabs.enabled && route.kind === ROUTE.designly && !wantsNewTab())
          flyComposer(FLY_FIELD.designly, FLY_FIELD.home, () => goTo(HOME));
        else navigate(HOME);
        setDrawerOpen(false);
      }}
      onSearch={() => {
        setDrawerOpen(false);
        setSearchOpen(true);
      }}
      onDesignly={() => {
        // From the start page the field flies up to Designly's.
        if (!tabs.enabled && route.kind === ROUTE.home && !wantsNewTab())
          flyComposer(FLY_FIELD.home, FLY_FIELD.designly, () => goTo({ kind: ROUTE.designly }));
        else navigate({ kind: ROUTE.designly });
        setDrawerOpen(false);
      }}
      onNotifications={(anchor) => toggleNotifications(anchor, 'right')}
      onToggleRail={toggleRail}
      onCloseDrawer={() => setDrawerOpen(false)}
      onPickMachine={pickMachine}
      onManageMachines={() => openScreen({ kind: ROUTE.settings, section: 'machines' })}
      people={<PeopleBlock machine={noV2 ? null : model.active} />}
      rail={
        <RailChats
          chats={noV2 ? NO_CHATS : model.chats}
          currentId={currentId}
          onOpen={(id, newTab) => {
            if (newTab) openInNewTab({ kind: ROUTE.chat, id }, true);
            else openChat(id);
          }}
        />
      }
    />
  );

  // Under the start page's field: where else the window goes.
  const toolOf = (id: string, icon: string, label: string, next: TRoute): IHomeTool => ({
    id,
    icon,
    label,
    onOpen: (newTab) => {
      if (newTab) openInNewTab(next, true);
      else navigate(next);
    },
  });
  const homeTools: readonly IHomeTool[] = [
    toolOf('designly', 'palette', t('next.sidebar.designly'), { kind: ROUTE.designly }),
    toolOf('apps', 'app-window', t('next.sidebar.apps'), { kind: ROUTE.apps }),
    {
      id: 'search',
      icon: 'search',
      label: t('next.sidebar.search'),
      onOpen: () => setSearchOpen(true),
    },
    toolOf('stats', 'chart-column', t('next.sidebar.stats'), { kind: ROUTE.stats }),
    toolOf('efficiency', 'gauge', t('next.sidebar.efficiency'), { kind: ROUTE.efficiency }),
    toolOf('settings', 'settings', t('next.shell.settings'), { kind: ROUTE.settings, section: null }),
  ];

  const overlays = (
    <>
      {projectDialog.dialog}
      {resumeDialog.dialog}
      <NotificationCenter
        open={notifications !== null}
        anchor={notifications?.anchor ?? null}
        side={notifications?.side ?? 'right'}
        items={notificationItems}
        failed={listFailed}
        now={now}
        onClose={() => setNotifications(null)}
        onOpenItem={(item) => {
          setNotifications(null);
          if (item.chatId) openChat(item.chatId);
          else setMachineReveal((n) => n + 1);
        }}
      />
      {search.mounted && (
        <LazyView
          load={loadSearchDialog}
          fallback={null}
          title={t('next.load.search')}
          onFail={(failure, retry) => {
            setSearchOpen(false);
            toast(t('next.load.search'), {
              tone: 'error',
              description: failure.message,
              action: {
                label: t('next.load.retry'),
                onClick: () => {
                  retry();
                  setSearchOpen(true);
                },
              },
            });
          }}
          props={{
            open: searchOpen,
            onClose: () => setSearchOpen(false),
            query,
            onQuery: setQuery,
            scopes: CHAT_SCOPES,
            scope,
            onScope: setScope,
            results: searchResults,
            failed: listFailed,
            indexing: messages.indexing,
            recent,
            onPick: (row: ISearchRow) => {
              setSearchOpen(false);
              setQuery('');
              if (row.target && isConvId(row.target.chatId)) {
                // The chat opens at the message: the feed of that chat takes the ask when it is on screen.
                revealMessage({
                  conv: row.target.chatId,
                  entryId: row.target.entryId,
                  turnId: row.target.turnId,
                });
                openChat(row.target.chatId);
              } else openChat(row.id);
            },
          }}
        />
      )}
      <NoticeStack
        notices={notices.notices}
        now={now}
        onOpen={notices.onOpen}
        onDismiss={notices.onDismiss}
        onDismissAll={notices.onDismissAll}
      />
      <SecretAskHost machine={boot.k === 'ready' ? model.active : null} />
      <ToastHost />
      <UpdateBanner available={update.available} onReload={update.reload} onDismiss={update.dismiss} />
      <ReleaseNotice release={update.available ? null : release.release} onDismiss={release.dismiss} />
      <WhatsNew ready={boot.k === 'ready'} updating={update.available} />
    </>
  );

  // The /goal is a Claude command: the item is in the menu of a Claude chat only.
  const headerGoal: IHeaderGoal | null =
    record && record.provider === 'claude'
      ? {
          current: record.goal ?? null,
          onSave: (text) => settled(setGoal(record.machineId, record.id, text)),
        }
      : null;

  const headerMachine = model.machine && {
    name: model.machine.name,
    health: model.machine.health,
    issue: model.machine.issue?.text ?? model.machine.alert?.text ?? null,
  };

  return (
    <RailFrame
      drawerOpen={drawerOpen}
      onDrawerChange={setDrawerOpen}
      sidebar={sidebar}
      tabs={tabs.strip}
      aside={boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat ? pane.drawer : null}
      overlays={overlays}
      textSize={me.k === 'ready' ? me.me.settings.textSize : undefined}
    >
      {/* The open design, the Apps screen and Settings have their own strip at the top: no header over them. */}
      {route.kind !== ROUTE.site &&
        route.kind !== ROUTE.invite &&
        route.kind !== ROUTE.apps &&
        route.kind !== ROUTE.settings && (
          <ChatHeader
            chat={chat}
            projects={model.projects}
            actions={model.actions}
            machine={headerMachine}
            drawerOpen={drawerOpen}
            onOpenDrawer={() => setDrawerOpen(true)}
            onNewChat={() => navigate(HOME)}
            pane={pane.header}
            terminal={term.header}
            background={bg.header}
            goal={headerGoal}
            parent={
              parentChat
                ? { title: parentChat.title, onOpen: () => navigate({ kind: ROUTE.chat, id: parentChat.id }) }
                : null
            }
            bell={{
              unread,
              open: notifications !== null,
              onToggle: (anchor) => toggleNotifications(anchor, 'bottom'),
            }}
          />
        )}
      {(boot.k === 'failed' || noV2) && <BootNotice kind={boot.k === 'failed' ? 'failed' : 'no-v2'} />}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.home && (
        <Greeting
          name={name}
          fromChat={screens.before === ROUTE.chat}
          onResume={resumeDialog.show}
          into={into}
          tools={<HomeTools tools={homeTools} />}
          composer={
            <HomeComposer
              machine={model.active}
              conversations={model.conversations}
              projectId={newChatInto?.id ?? null}
              onCreated={(id) => {
                newChatProjectStore.set(null);
                goTo({ kind: ROUTE.chat, id });
              }}
            />
          }
        />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat && (
        <ChatStage
          key={stageId}
          leaving={leaving}
          fromHome={arrival.fromHome}
          side={bg.panel}
          paneOpen={pane.open}
          missing={missing === currentId && !present ? { onHome: () => goTo(HOME) } : null}
        >
          {present && isConvId(stageId) && chatMachine ? (
            <ChatView
              conv={stageId}
              machine={chatMachine}
              onOpenFile={pane.openFile}
              onOpenOutput={pane.openOutput}
              onOpenRef={pane.openRef}
              onShowBackground={bg.show}
              onOpenTerminal={term.reveal}
              onRetryLoad={() => {
                // The channel is asked again: the feed's own error is cleared by the next snapshot.
                hub.client(chatMachine)?.reconnectNow();
              }}
            />
          ) : (
            <QuietChat />
          )}
        </ChatStage>
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat && term.panel}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat && pane.sideFloat}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.designly && model.active && (
        <DesignlyView key={model.active} machine={model.active} />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.designlyNew && model.active && (
        <DeckCreateView key={model.active} machine={model.active} />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.apps && model.active && (
        <AppsScreen key={model.active} machine={model.active} onOpenDrawer={() => setDrawerOpen(true)} />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.settings && model.active && (
        <SettingsScreen
          key={model.active}
          machine={model.active}
          section={route.section}
          name={name ?? login}
          onOpenDrawer={() => setDrawerOpen(true)}
        />
      )}
      {route.kind === ROUTE.stats && <LegacyScreen screen={LEGACY_SCREEN.stats} />}
      {route.kind === ROUTE.efficiency && <LegacyScreen screen={LEGACY_SCREEN.efficiency} />}
      {route.kind === ROUTE.invite && <InviteAccept key={route.token} token={route.token} />}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.site && !model.active && <SiteWait />}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.site && model.active && (
        <RailSite
          key={`${model.active}/${route.id}/${tabs.activeId}`}
          machine={model.active}
          id={route.id}
          chat={route.chat ?? null}
          access={route.access ?? null}
          onToggleMax={toggleRail}
          onOpenDrawer={() => setDrawerOpen(true)}
          onMissing={() => goTo({ kind: ROUTE.designly }, { replace: true })}
        />
      )}
    </RailFrame>
  );
};

export default Shell;
