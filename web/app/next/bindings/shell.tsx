// The new interface's shell on the v2 stores: the frame, the sidebar, the header and the open chat. The
// components take props; the models in this folder turn the stores into them. A conversation is drawn from the
// moment its id is in the address — its feed is the store's, whatever the sockets do.
import '@/next/styles/tokens.css';
import '@/next/styles/palettes.css';
import '@/next/styles/motion.css';
import '@/next/styles/base.css';
import '@/next/styles/icons.css';
import '@/next/styles/ui.css';
import '@/next/styles/shell.css';
import '@/next/styles/sidebar.css';
import '@/next/styles/header.css';
import '@/next/styles/tabs.css';

import { createPortal } from 'react-dom';
import {
  Activity,
  memo,
  useLayoutEffect,
  lazy,
  Suspense,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
  type ComponentProps,
} from 'react';
import { appConfig } from '@/platform/config';
import { startMachineOf } from '@/next/bindings/start-machine';
import { QueuedChatScreen } from '@/next/bindings/queued-chat';
import { queuedChatId, queuedItemOf } from '@/next/commands/queued-chats';
import { readLastChoice } from '@/next/features/composer/start-choice';
import { useTheme } from '@/platform/theme';
import { useAgentTerminal } from '@/next/bindings/agent-terminal';
import { useRightPane } from '@/next/bindings/right-pane';
import { useHomePane } from '@/next/bindings/home-pane';
import KeptAppFrames from '@/next/features/app-views/kept-frames';
import { useChatBackground } from '@/next/bindings/chat-background';
import { useCentralHistory } from '@/next/bindings/use-central';
import { useProfileSidebarMerge } from '@/next/bindings/use-profile-sidebar';
import ChatView from '@/next/bindings/chat-view';
import HomeComposer from '@/next/bindings/home-composer';
import {
  machineProjectOf,
  machineSwitchLeavesChat,
  shownCard,
  useShellModel,
  withLoad,
  type IShownMachine,
  useTabChats,
} from '@/next/bindings/shell-model';
import { bootNext, shutdownNext, useBoot } from '@/next/boot';
import { interrupt, resumeConversation, setGoal } from '@/next/commands';
import { useReadWhileVisible } from '@/next/bindings/read-while-visible';
import { connectionStore } from '@/next/stores/connection';
import { useAll } from '@/next/core/store';
import { hub } from '@/next/core/realtime/hub';
import ChatHeader, { type IChatHeaderProps } from '@/next/features/header/chat-header';
import type { IHeaderGoal } from '@/next/features/header/goal-dialog';
import { settled } from '@/next/bindings/outcome';
import { BackgroundButton, TerminalButton } from '@/next/features/header/header-buttons';
import { ChatTerminalContext, ChatTerminalSlotContext } from '@/next/features/terminal/chat-terminal';
import NotificationCenter from '@/next/features/notifications/notification-center';
import { readOpened, useNotificationCenter } from '@/next/bindings/notification-center';
import { BrowserProfileAskHost } from '@/next/bindings/browser-profile-asks';
import { BrowserLoginAskHost } from '@/next/bindings/browser-login-asks';
import { SecretAskHost } from '@/next/bindings/safe-asks';
import { useNotices } from '@/next/bindings/notices';
import { useTurnSummaries } from '@/next/bindings/turn-summary';
import SmartNoticeHost, { noticeQuiet } from '@/next/bindings/smart-notice';
import { setHistoryPerson } from '@/next/stores/notice-history';
import { ROW_GO } from '@/next/features/search/search-dialog';
import { SCOPE } from '@/next/features/search/smart-rows';
import { APP_ROW, appSearchGroup } from '@/next/features/search/app-rows';
import { useInstalledApps } from '@/next/features/app-views/api';
import { openAppTab } from '@/next/features/app-views/open';
import { isSpaceApp } from '@/next/features/app-views/service-health';
import { designPlaceOf } from '@/next/features/designly/chat-place';
import { setDraftText } from '@/next/stores/drafts';
import { useSmartSearch } from '@/next/bindings/use-smart-search';
import {
  hasTestChats,
  toggleShowTestChats,
  useShowTestChats,
  withoutTestChats,
} from '@/next/features/sidebar/test-chats';
import { revealMessage } from '@/next/stores/reveal';
import type { ISearchRow } from '@/next/features/search/search-dialog';
import AppFrame, { type IAppFrameProps } from '@/next/features/shell/app-frame';
import {
  RAIL_MODE,
  foldRailForPane,
  toggleRail,
  useRail,
  useRailFull,
  useRailMode,
} from '@/next/bindings/rail';
import { usePaneOpen } from '@/next/bindings/pane-open';
import { useChatScreen } from '@/next/bindings/use-chat-screen';
import { useChatViewSync } from '@/next/bindings/use-chat-view-sync';
import ChatStage from '@/next/features/shell/chat-stage';
import LookLayer from '@/next/features/looks/look-layer';
import AgentLooks from '@/next/features/looks/agent-looks';
import LookSheet from '@/next/features/looks/look-sheet';
import QuietChat from '@/next/bindings/quiet-chat';
import { PHONE_QUERY } from '@/next/features/shell/gestures';
import Greeting from '@/next/features/shell/greeting';
import { MachineSetup } from '@/next/bindings/machine-setup';
import HomeTools, { type IHomeTool } from '@/next/features/shell/home-tools';
import { PeopleBlock } from '@/next/bindings/asks';
import { useShellTabs, wantsNewTab } from '@/next/bindings/tabs';
import { useShellKeys } from '@/next/bindings/shell-keys';
import { keepScreensOnPhone, TabScopeContext, useTabs, useTopTabs } from '@/next/stores/tabs';
import { deviceKeptScreensMax, keptScreens } from '@/next/features/shell/kept-screens';
import {
  CHAT_RECORD_WAIT_MS,
  screenChatOf,
  screenMachine,
  screenWaits,
} from '@/next/features/shell/screen-machine';
import { keptBudget } from '@/next/features/shell/kept-budget';
import { usePhone } from '@/next/ui/use-media';
import Sidebar from '@/next/features/sidebar/sidebar';
import { useSidebarApps } from '@/next/bindings/use-sidebar-apps';
import {
  AppAboutPage,
  AppDocPage,
  AppMainPage,
  AppsListPage,
  AppsScreen,
  DesignlySite,
  DesignlyView,
  HelpPage,
  InviteAccept,
  KitPage,
  ReviewAppPage,
  SettingsScreen,
  isWarmPage,
  warmPage,
  type TWarmPage,
} from '@/next/bindings/later-pages';
import { appStageDesign } from '@/next/features/app-main/app-stage';
import { AppConfirmHost } from '@/next/features/app-views/confirm';
import { AppFilePickHost } from '@/next/features/app-views/file-pick';
import LegacyScreen, { LEGACY_SCREEN, ManageScreen } from '@/next/legacy/screens';
import {
  SIDEBAR_VIEW,
  type IMoreItem,
  type ISidebarChat,
  type ISidebarProject,
} from '@/next/features/sidebar/types';
import Icon from '@/next/ui/icon';
import { useHubEntry } from '@/next/features/sidebar/hub-entry';
import { useBeta } from '@/next/stores/beta';
import LazyView from '@/next/ui/lazy-view';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { goTo, HOME, parseRoute, ROUTE, routePath, useRoute, type TRoute } from '@/next/routes/routes';
import { BrandingHost, HomeSlots, useHomeStage } from '@/next/features/app-views/slot-pages';
import { DESIGN_KIND } from '@/next/features/designly/design-kind';
import { useWorkingDesigns } from '@/next/bindings/working-designs';
import { useDesignBound } from '@/next/bindings/design-conv';
import { designKey } from '@/next/features/designly/design-conv';
import SiteWait from '@/next/features/designly/site-wait';
import { useProjectDialog, useProjectSettingsDialog, useResumeDialog } from '@/next/bindings/shell-dialogs';
import { useAppUpdate } from '@/next/bindings/use-app-update';
import { useRelease } from '@/next/bindings/use-release';
import ReleaseNotice from '@/next/features/update/release-notice';
import UpdateBanner from '@/next/features/update/update-banner';
import RolloutBanner from '@/next/features/rollout/rollout-banner';
import LearnPage from '@/next/features/learn/learn-page';
import ChatGptScreen from '@/next/features/chatgpt/chat-screen';
import { askVoiceStart, markWakeStart, voiceNow } from '@/next/features/voice-live/field';
import VoiceLayer from '@/next/features/voice-live/voice-layer';
import { gptChatsChanged, useGptChats, useGptSignedIn } from '@/next/features/chatgpt/store';
import { gptIdOf } from '@/next/features/chatgpt/voice-bridge';
import { deleteChat as deleteGptChat, renameChat as renameGptChat } from '@/next/features/chatgpt/api';
import LearnWelcome from '@/next/features/learn/learn-welcome';
import TourOverlay from '@/next/features/tour/tour-overlay';
import WhatsNew from '@/next/features/whats-new/whats-new';
import { REFS_MAX, type Conversation, type ConvId, type MachineId } from '@/next/protocol/v2';
import { chatRef } from '@/next/features/composer/chat-sources';
import { addDraftRef } from '@/next/stores/drafts';
import { conversationsStore, useConversation, useConvsAnswered } from '@/next/stores/conversations';
import NextChatPull from '@/next/features/chat/next-chat-pull';
import usePaneEdgeSwipe from '@/next/features/shell/use-pane-edge-swipe';
import { linkReason, useConnection } from '@/next/stores/connection';
import { patchSettings, useMe, type NavSettings } from '@/next/stores/me';
import { useHomeMachine, useMachineLoad } from '@/next/stores/machines';
import { useCentral } from '@/next/stores/central';
import { useFeed } from '@/next/stores/conversation';
import {
  newChatMachineStore,
  newChatProjectStore,
  resetHome,
  useNewChatMachine,
  useNewChatProject,
} from '@/next/stores/new-chat';
import { UI_REQUEST, useUiRequest } from '@/next/stores/ui-requests';
import { ToastHost, toast } from '@/next/ui/toast';
import { useDeadline } from '@/next/ui/use-deadline';
import { FLY_FIELD, flyComposer } from '@/next/ui/fly-composer';
import { countRender } from '@/next/core/perf-journal';

/** ⌥ Space away from a chat: the voice's field over the screen, loaded when asked. */
const VoiceFloat = lazy(() => import('@/next/features/voice-live/voice-float'));
const VoiceBar = lazy(() => import('@/next/features/voice-live/voice-bar'));
const VoiceFull = lazy(() => import('@/next/features/voice-live/voice-full'));
const WakeWord = lazy(() => import('@/next/features/voice-live/wake-word'));

const CHAT_LEAVE_MS = 180;
const NO_CHATS: readonly ISidebarChat[] = [];
const NO_PROJECTS: readonly ISidebarProject[] = [];
const NO_ROWS: readonly ISearchRow[] = [];

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
const ChatNavigator = lazy(() => import('@/next/bindings/chat-navigator'));
const loadSearchDialog = () => import('@/next/features/search/search-dialog');

/** A chat named by the address that the list does not have yet (a tab started a second ago) is waited for this long. */
const CHAT_WAIT_MS = 15_000;
/** The day groups and «5 мин» move on with this clock. */
const CLOCK_MS = 60_000;

/** The frame with the rail read here, so that folding the sidebar draws the frame and not the shell around it. */
const RailFrame = (props: Omit<IAppFrameProps, 'rail' | 'full'>) => (
  <AppFrame {...props} rail={useRail()} full={useRailFull()} />
);

/**
 * The right pane opening on a narrow window folds the open sidebar; its shutting leaves it folded: only the
 * person opens the sidebar. Its own component, so that the pane's opening draws nothing of the shell.
 */
const RailFollowsPane = () => {
  const paneOpen = usePaneOpen();
  useEffect(() => {
    if (paneOpen && !window.matchMedia(PHONE_QUERY).matches) foldRailForPane(window.innerWidth);
  }, [paneOpen]);
  return null;
};

/** The Designly site's stage, which gives its room back to the page when the sidebar is folded. */
const RailSite = (props: Omit<ComponentProps<typeof DesignlySite>, 'maxed'>) => (
  <DesignlySite {...props} maxed={useRail()} />
);

interface IKeptScreensProps {
  machine: MachineId;
  route: TRoute;
  activeId: string;
  terminal: ComponentProps<typeof ChatTerminalContext.Provider>['value'];
  onTermSlot: (slot: HTMLElement | null) => void;
  toolPane: ComponentProps<typeof DesignlySite>['toolPane'];
  open: ComponentProps<typeof DesignlySite>['open'];
  onOpenDrawer: () => void;
}

/** What a tab shows: its address, or the design its chat edits (a tab is moved to the chat's address first). */
const screenOfPath = (path: string): TRoute => designPlaceOf(parseRoute(path));

/**
 * The screen a kept tab stands for (features/shell/kept-screens): a design, an app's space (the boards, the documents),
 * a document, a review, a kit; null for the screens that hold no frame and are drawn anew each time. It names what the
 * screen's page is drawn anew for (its key in KeptPage): two addresses of one screen (another page of the design) are
 * one screen.
 */
const keptScreenOf = (route: TRoute): string | null => {
  if (route.kind === ROUTE.site) return `site:${route.id}`;
  // The spaces are one screen whatever is open in them: a board, a document, their lists.
  if (route.kind === ROUTE.app)
    return isSpaceApp(route.id) ? 'app:spaces' : `app:${route.id}/${route.object ?? ''}/${route.chat ?? ''}`;
  if (route.kind === ROUTE.appDoc) return `doc:${route.id}`;
  if (route.kind === ROUTE.review && route.url !== undefined)
    return `review:${route.url}/${route.chat ?? ''}`;
  if (route.kind === ROUTE.kit) return `kit:${route.id}/${route.version ?? ''}/${route.theme ?? ''}`;
  return null;
};
const isKeptPath = (path: string) => keptScreenOf(screenOfPath(path)) !== null;
/** How many screens this device keeps mounted (features/shell/kept-screens). */
const KEPT_MAX = deviceKeptScreensMax();
// The phone shows no tabs: a screen left for another place waits in a tab of its own, and «Назад» finds it standing.
keepScreensOnPhone({ keeps: isKeptPath, max: KEPT_MAX + 1 });

interface IKeptScreenProps {
  tabId: string;
  active: boolean;
  machine: MachineId;
  /** The screen's address, as a string: the same one draws nothing anew. */
  path: string;
  /** Only the screen on view has them; a hidden one gets null, so nothing of the shell draws it again. */
  terminal: IKeptScreensProps['terminal'];
  onTermSlot: IKeptScreensProps['onTermSlot'] | null;
  toolPane: IKeptScreensProps['toolPane'];
  /** The shell's callbacks, read when called: a new function each shell render would draw every hidden screen again. */
  shell: { current: Pick<IKeptScreensProps, 'open' | 'onOpenDrawer'> };
}

/** The page of one kept screen, by its kind. A hidden one moves no address: only the screen on view does. */
const KeptPage = ({
  route,
  active,
  machine,
  toolPane,
  open,
  shell,
}: {
  route: TRoute;
  active: boolean;
  machine: MachineId;
  toolPane: IKeptScreensProps['toolPane'];
  open: NonNullable<IKeptScreensProps['open']>;
  shell: IKeptScreenProps['shell'];
}) => {
  const onOpenDrawer = () => shell.current.onOpenDrawer();
  if (route.kind === ROUTE.site)
    return (
      <RailSite
        key={`${machine}/${route.id}`}
        machine={machine}
        id={route.id}
        chat={route.chat ?? null}
        access={route.access ?? null}
        edit={route.edit ?? false}
        onMode={(edit) => {
          if (!active) return;
          goTo({ ...route, ...(edit ? { edit: true } : { edit: undefined }) }, { replace: true });
        }}
        toolPane={toolPane}
        open={open}
        onToggleMax={null}
        onOpenDrawer={onOpenDrawer}
        onMissing={() => {
          if (active) goTo({ kind: ROUTE.designly }, { replace: true });
        }}
      />
    );
  if (route.kind === ROUTE.app)
    return (
      <AppMainPage
        key={isSpaceApp(route.id) ? 'spaces' : `${route.id}/${route.object ?? ''}/${route.chat ?? ''}`}
        machine={machine}
        id={route.id}
        object={route.object ?? null}
        thread={route.thread ?? null}
        chat={route.chat ?? null}
        open={open}
        toolPane={toolPane}
        onOpenDrawer={onOpenDrawer}
      />
    );
  if (route.kind === ROUTE.review)
    return (
      <ReviewAppPage
        key={`${route.url ?? ''}/${route.chat ?? ''}`}
        machine={machine}
        url={route.url ?? null}
        chat={route.chat ?? null}
        wt={route.wt ?? null}
        open={open}
        toolPane={toolPane}
        onOpenDrawer={onOpenDrawer}
      />
    );
  if (route.kind === ROUTE.appDoc)
    return (
      <AppDocPage
        key={`${machine}/${route.id}`}
        machine={machine}
        id={route.id}
        chat={route.chat ?? null}
        edit={route.edit ?? false}
        onMode={(edit) => {
          if (!active) return;
          goTo({ ...route, ...(edit ? { edit: true } : { edit: undefined }) }, { replace: true });
        }}
        onOpenDrawer={onOpenDrawer}
      />
    );
  if (route.kind === ROUTE.kit)
    return (
      <KitPage
        key={`${machine}/${route.id}/${route.version ?? ''}/${route.theme ?? ''}`}
        machine={machine}
        route={route}
      />
    );
  return null;
};

/**
 * One kept screen: drawn again only when its own props change, so a hidden one stays still while the shell renders.
 * Hidden by React's Activity: its DOM, frames and state stay, its effects (loops that follow boxes, listeners, polls)
 * are cleaned up until it is shown again, so a hidden screen costs no CPU.
 */
const KeptScreen = memo(
  ({ tabId, active, machine, path, terminal, onTermSlot, toolPane, shell }: IKeptScreenProps) => {
    const tabRoute = parseRoute(path);
    // The screen is drawn where its chat is, not on the machine active in the shell: a chat is one entity. While nothing
    // says where the chat is (its record and feed are still coming) the screen waits, it does not fall back to the
    // active machine, whose list has not got the chat (a new chat would be offered for an existing one).
    const chatId = screenChatOf(tabRoute);
    const chatRecord = useConversation(chatId && isConvId(chatId) ? chatId : null);
    const chatFeed = useFeed(chatId && isConvId(chatId) ? chatId : null);
    const chatOn = chatRecord?.machineId ?? chatFeed?.machineId ?? null;
    const waits = screenWaits(tabRoute, chatOn);
    const [patient, setPatient] = useState(true);
    useEffect(() => {
      if (!waits) return;
      const timer = setTimeout(() => setPatient(false), CHAT_RECORD_WAIT_MS);
      return () => clearTimeout(timer);
    }, [waits]);
    const screenOn = screenMachine(tabRoute, chatOn, machine) as MachineId;
    const scope = useMemo(() => ({ tabId, active }), [tabId, active]);
    // The pane's openers, the same object for the screen's life: each one calls the shell's current one.
    const open = useMemo<NonNullable<IKeptScreensProps['open']>>(
      () => ({
        onOpenRef: (...args) => shell.current.open?.onOpenRef?.(...args),
        onOpenFile: (...args) => shell.current.open?.onOpenFile?.(...args),
        onOpenOutput: (...args) => shell.current.open?.onOpenOutput?.(...args),
      }),
      [shell],
    );
    if (keptScreenOf(tabRoute) === null) return null;
    const loading = waits && patient;
    return (
      <div className="hx-kept-screen" data-on={active} inert={!active}>
        <Activity mode={active ? 'visible' : 'hidden'}>
          <TabScopeContext.Provider value={scope}>
            <ChatTerminalContext.Provider value={terminal}>
              <ChatTerminalSlotContext.Provider value={onTermSlot}>
                {loading ? (
                  <SiteWait />
                ) : (
                  <KeptPage
                    route={tabRoute}
                    active={active}
                    machine={screenOn}
                    toolPane={toolPane}
                    open={open}
                    shell={shell}
                  />
                )}
              </ChatTerminalSlotContext.Provider>
            </ChatTerminalContext.Provider>
          </TabScopeContext.Provider>
        </Activity>
      </div>
    );
  },
);

/**
 * The screens of the last tabs that hold a frame stay mounted (features/shell/kept-screens): a design, an app's space
 * (the boards, the documents), a document, a review, a kit. The one on screen is in the page's flow, the others hidden
 * in the same place (KeptScreen). A tab switched back to shows its screen at once, frames and chat as they were. Each
 * stays under this one parent for its life: a frame moved in the document would load again.
 */
const KeptScreens = ({
  machine,
  route,
  activeId,
  terminal,
  onTermSlot,
  toolPane,
  open,
  onOpenDrawer,
}: IKeptScreensProps) => {
  const state = useTabs();
  const [kept, setKept] = useState<readonly string[]>([]);
  const shell = useRef({ open, onOpenDrawer });
  useLayoutEffect(() => {
    shell.current = { open, onOpenDrawer };
  });
  const screen = keptScreenOf(route);
  const onView = screen !== null ? activeId : '';
  // The page's one budget of frames out of sight (features/shell/kept-budget): the pane's views and browsers count too.
  const over = useSyncExternalStore(keptBudget.subscribe, keptBudget.over, keptBudget.over);
  const allowed = (id: string) => id === onView || !over.has(`screen:${id}`);
  const next = keptScreens(
    kept.every(allowed) ? kept : kept.filter(allowed),
    onView,
    state.tabs,
    isKeptPath,
    KEPT_MAX,
  );
  if (next !== kept) setKept(next);
  // When each was last on screen, told to the budget.
  const seen = useRef(new Map<string, number>());
  useLayoutEffect(() => {
    const now = Date.now();
    if (onView !== '') seen.current.set(onView, now);
    for (const id of [...seen.current.keys()]) if (!next.includes(id)) seen.current.delete(id);
    keptBudget.report(
      'screens',
      next.map((id) => ({ key: `screen:${id}`, shown: id === onView, at: seen.current.get(id) ?? now })),
    );
  });
  return (
    <>
      {/* In a fixed order, not the recent first: React moving a screen in the document would load its frames again. */}
      {[...next].sort().map((tabId) => {
        const active = screen !== null && tabId === activeId;
        const fromTab = screenOfPath(state.tabs.find((tab) => tab.id === tabId)?.path ?? '/');
        // The address is the tab's only once it names the same screen: right after a switch the tab is active while
        // the address still names the screen left, and a screen given that one would be drawn anew as it.
        const tabRoute = active && screen === keptScreenOf(fromTab) ? route : fromTab;
        return (
          <KeptScreen
            key={tabId}
            tabId={tabId}
            active={active}
            machine={machine}
            path={routePath(tabRoute)}
            terminal={active ? terminal : null}
            onTermSlot={active && terminal ? onTermSlot : null}
            toolPane={active ? toolPane : null}
            shell={shell}
          />
        );
      })}
    </>
  );
};

/** The cluster's copy of the chats follows the links: asked again when one goes down or comes back. */
const useCentralList = () => {
  const links = useAll(connectionStore, (c) =>
    [...c.keys()]
      .map((m) => `${m}:${c.get(m)?.k}`)
      .sort()
      .join(','),
  );
  useCentralHistory(links);
  useProfileSidebarMerge();
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

/**
 * How many chats stay mounted after they are left: enough for going back and forth between a few on a
 * computer. A phone, a device that keeps two screens or one with little memory keeps only the chat on view:
 * in Safari every hidden chat's feed weighed on each open and close of the drawer (worst frame 123 → 460 ms).
 */
const KEPT_CHATS_WIDE = 3;
const LOW_MEMORY_GB = 4;
/** A phone, a device that keeps two screens or one with little memory: it spends nothing ahead. */
const weakDevice = (): boolean => {
  if (typeof window === 'undefined' || KEPT_MAX <= 2) return true;
  const memory = (navigator as Navigator & { deviceMemory?: number }).deviceMemory;
  return window.matchMedia(PHONE_QUERY).matches || (memory !== undefined && memory <= LOW_MEMORY_GB);
};
const keptChatsMax = (): number => (weakDevice() ? 1 : KEPT_CHATS_WIDE);

/** A computer fetches Settings and Sinopia a while after the start, once idle: long enough not to slow the first chat. */
const WARM_IDLE_MS = 5000;
const IDLE_PAGES: readonly TWarmPage[] = ['settings', 'designly'];
/**
 * The pages the sidebar opens come ahead of the first click: on a computer when the page is idle, everywhere
 * when the person points at the item (hover, a touch, focus). A phone fetches only on such an intent: in idle time its
 * parse would cost the frames of whatever moves next.
 */
const useWarmPages = () => {
  useEffect(() => {
    const onIntent = (event: Event) => {
      const name = (event.target as Element | null)?.closest?.<HTMLElement>('[data-warm]')?.dataset.warm;
      if (isWarmPage(name)) warmPage(name);
    };
    const events = ['pointerover', 'pointerdown', 'focusin'] as const;
    for (const type of events) document.addEventListener(type, onIntent, { capture: true, passive: true });
    let timer = 0;
    if (!weakDevice())
      timer = window.setTimeout(() => {
        const idle = (run: () => void) => {
          if (typeof window.requestIdleCallback === 'function') window.requestIdleCallback(run);
          else setTimeout(run, 0);
        };
        const next = (at: number) => {
          if (at >= IDLE_PAGES.length) return;
          idle(() => {
            warmPage(IDLE_PAGES[at]);
            next(at + 1);
          });
        };
        next(0);
      }, WARM_IDLE_MS);
    return () => {
      window.clearTimeout(timer);
      for (const type of events) document.removeEventListener(type, onIntent, { capture: true });
    };
  }, []);
};

interface IKeptChat {
  id: ConvId;
  machine: MachineId;
  /** When it was on the stage last, in the order of opening: the one seen longest ago leaves first. */
  seen: number;
}

/** The kept chats with this one on the stage. Their order stays: moving a hidden chat's node lays it out anew. */
const keepChat = (
  kept: readonly IKeptChat[],
  chat: { id: ConvId; machine: MachineId },
  seen: number,
): readonly IKeptChat[] => {
  const now = { ...chat, seen };
  const max = keptChatsMax();
  // A window that became a phone lets go of the chats it kept beyond its share, the ones seen longest ago first.
  const newest = [...kept]
    .filter((k) => k.id !== chat.id)
    .sort((a, b) => b.seen - a.seen)
    .slice(0, max - 1);
  const stays = new Set([...newest.map((k) => k.id), chat.id]);
  const left = kept.filter((k) => stays.has(k.id)).map((k) => (k.id === chat.id ? now : k));
  if (left.some((k) => k.id === chat.id)) return left;
  if (left.length < kept.length) {
    // Into the place of one that left: the order of the rest stays.
    const at = kept.findIndex((k) => !stays.has(k.id));
    const placed = kept
      .map((k, index) => (index === at ? now : k))
      .filter((k) => stays.has(k.id) || k === now);
    return placed;
  }
  return [...left, now];
};

interface IChatCalls {
  openFile: ComponentProps<typeof ChatView>['onOpenFile'];
  openOutput: ComponentProps<typeof ChatView>['onOpenOutput'];
  openRef: ComponentProps<typeof ChatView>['onOpenRef'];
  showBackground: ComponentProps<typeof ChatView>['onShowBackground'];
  openTerminal: ComponentProps<typeof ChatView>['onOpenTerminal'];
}

/** One kept chat: its props do not change while it waits hidden, so the shell drawing again leaves it be. */
const KeptChat = memo(
  ({ conv, machine, calls }: { conv: ConvId; machine: MachineId; calls: { current: IChatCalls | null } }) => (
    <ChatView
      conv={conv}
      machine={machine}
      onOpenFile={(...args) => calls.current?.openFile?.(...args)}
      onOpenOutput={(...args) => calls.current?.openOutput?.(...args)}
      onOpenRef={(...args) => calls.current?.openRef?.(...args)}
      onShowBackground={(...args) => calls.current?.showBackground?.(...args)}
      onOpenTerminal={() => calls.current?.openTerminal?.()}
      onRetryLoad={() => {
        // The channel is asked again: the feed's own error is cleared by the next snapshot.
        hub.client(machine)?.reconnectNow();
      }}
    />
  ),
);

/** The chat whose right pane is beside the screen: the open chat, or the open design's chat. */
const paneConvOf = (
  route: TRoute,
  chat: Conversation | null,
  design: Conversation | null,
): Conversation | null => {
  if (route.kind === ROUTE.chat) return chat;
  if (route.kind === ROUTE.site || route.kind === ROUTE.app) return design;
  return null;
};

/**
 * The view the right pane stands beside, when it is not the chat's own page: each has a pane of its own —
 * the documents' is not the boards', a design's is not its chat's.
 */
const paneViewOf = (route: TRoute): string | null => {
  if (route.kind === ROUTE.site) return `site:${route.id}`;
  if (route.kind === ROUTE.app) return `app:${route.id}`;
  return null;
};

/** The screens that take the whole window, where the sidebar starts folded. */
const railModeOf = (route: TRoute) =>
  route.kind === ROUTE.app ||
  (route.kind === ROUTE.review && route.url !== undefined) ||
  route.kind === ROUTE.site ||
  route.kind === ROUTE.appDoc ||
  route.kind === ROUTE.kit
    ? RAIL_MODE.full
    : RAIL_MODE.screen;

/** Which of the sidebar's screens is on: Designly and its designs, Settings, or the chats. */
const sidebarView = (route: TRoute) => {
  if (
    route.kind === ROUTE.designly ||
    route.kind === ROUTE.designlyNew ||
    route.kind === ROUTE.videoNew ||
    route.kind === ROUTE.site ||
    route.kind === ROUTE.kit ||
    route.kind === ROUTE.appDoc ||
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

/**
 * The header with the machine's card whose meters read the load here: the active machine reports it every
 * 2 s, and a reading draws this header alone, not the shell with the sidebar and the chat.
 */
const LoadedChatHeader = ({ shown, ...props }: IChatHeaderProps & { shown: IShownMachine }) => {
  const load = useMachineLoad(shown.view?.machine.id ?? null);
  const card = useMemo(
    () =>
      shown.view && load ? shownCard(withLoad(shown.view, load), shown.lost, shown.down) : props.machine.card,
    [shown, load, props.machine.card],
  );
  return (
    <ChatHeader
      {...props}
      machine={card === props.machine.card ? props.machine : { ...props.machine, card }}
    />
  );
};

/** The menu the person laid out by hand in the rail: kept in their settings, the same on every device. */
const saveNav = async (nav: NavSettings): Promise<void> => {
  const res = await patchSettings({ nav });
  if (res.r !== 'accepted') throw new Error(res.r);
};

const Shell = ({ owner }: IShellProps) => {
  countRender('shell');
  const model = useShellModel();
  const boot = useBoot();
  const asked = useRoute();
  // A chat that edits a design is that design with its chat (chat-place.ts): shown so from the first frame, so no plain
  // chat with the sidebar flashes, and the address follows.
  // useRoute keeps the object while the address is the same: the address is what moves these.
  const route = useMemo(() => designPlaceOf(asked), [asked]);
  useWarmPages();
  useEffect(() => {
    if (route !== asked) goTo(route, { replace: true });
  }, [asked, route]);
  const me = useMe();
  // The colour of what pops up, the person's setting: until it comes, the default.
  const now = useClock();
  useCentralList();
  const update = useAppUpdate();
  const release = useRelease(model.active);
  const currentId = route.kind === ROUTE.chat ? route.id : null;
  // a new chat that waits in the hub for its machine has an address of its own.
  const queuedOpen = currentId === null ? null : queuedItemOf(currentId);
  // An app, a design, an app's document and a kit take the whole window: the sidebar there keeps its own choice,
  // folded until the person opens it (rail.ts).
  useRailMode(railModeOf(route));
  // A chat opens on the full screen it was left on.
  useChatScreen(route);
  // …and keeps its view on the machine, for the person's other devices.
  useChatViewSync(route);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [notifications, setNotifications] = useState<INotificationsState | null>(null);
  const [searchOpen, setSearchOpen] = useState(false);
  // The dialog stays mounted through its exit (the scrim and the box fade out in CSS), then goes.
  const search = usePresentFlag(searchOpen, SEARCH_EXIT_MS);
  const [query, setQuery] = useState('');
  const [scope, setScope] = useState<string>(SCOPE.all);
  const login = owner.split('@')[0] ?? '';
  // The tabs over the window: a browser's, on a wide window, unless switched off in Settings.
  const phone = usePhone();
  const hubEntry = useHubEntry();
  // «Hub first» (Settings → Beta): the runtime is a place to work with agents; Tasks, MR and Help are gone, Management
  // lives in the hub.
  const { hubFirst } = useBeta();
  const tabs = useShellTabs(useTabChats(), useTopTabs() && !phone);
  const name = (me.k === 'ready' ? me.me.name : null) ?? appConfig().people[login] ?? null;

  // While the profile loads there is no name to show: the footer draws a placeholder, not the login.
  const loginName = me.k === 'loading' ? null : login;
  const personName = name ?? loginName;
  const person = personName === null ? null : { name: personName };

  const { navigate, openInNewTab } = tabs;
  // ⌥ Space from anywhere: the voice in the field of the chat on screen; elsewhere the same field rises over the screen
  // and what is said goes to the last chat.
  const lastChat = useMemo(
    () =>
      [...(model.conversations ?? [])]
        .filter((c) => !c.parentId)
        .sort((a, b) => b.lastActivityAt - a.lastActivityAt)[0] ?? null,
    [model.conversations],
  );
  const [voiceFloat, setVoiceFloat] = useState<{ machine: MachineId; conv: ConvId } | null>(null);
  // «Привет, harnessly» (wake-word.tsx) starts it the same way.
  const startVoice = useCallback(
    (byWake: boolean) => {
      if (byWake) markWakeStart();
      const inChat = route.kind === ROUTE.chat || route.kind === ROUTE.chatgpt;
      if (inChat) return askVoiceStart();
      if (lastChat) setVoiceFloat((was) => was ?? { machine: lastChat.machineId, conv: lastChat.id });
    },
    [route.kind, lastChat],
  );
  const startByWake = useCallback(() => startVoice(true), [startVoice]);
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (!event.altKey || event.ctrlKey || event.metaKey || event.code !== 'Space') return;
      event.preventDefault();
      // During a talk ⌥ Space goes back to its chat (it belongs to the chat it was started in).
      const talk = voiceNow();
      if (talk?.conv) {
        if (!talk.host || talk.host.getClientRects().length === 0)
          navigate({ kind: ROUTE.chat, id: talk.conv });
        return;
      }
      startVoice(false);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [startVoice, navigate]);
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

  // With «Hub first» the retired screens' addresses lead home; Management goes to the hub (home when there is none).
  const retired =
    hubFirst &&
    (route.kind === ROUTE.tasks ||
      route.kind === ROUTE.help ||
      route.kind === ROUTE.manage ||
      route.kind === ROUTE.stats ||
      route.kind === ROUTE.efficiency);
  const managed =
    route.kind === ROUTE.manage || route.kind === ROUTE.stats || route.kind === ROUTE.efficiency;
  useEffect(() => {
    if (!retired) return;
    if (managed && hubEntry) window.location.replace(hubEntry);
    else goTo(HOME, { replace: true });
  }, [retired, managed, hubEntry]);

  // Switching chats, the open one leaves first (its thread fades up, 180 ms), then the next one comes in.
  const stageId = useLeaving(currentId, CHAT_LEAVE_MS);
  const leaving = stageId !== currentId;
  // The start page's field rises from the chat's place only when a chat was the screen before it.
  const [screens, setScreens] = useState<{ now: TRoute['kind']; before: TRoute['kind'] | null }>({
    now: route.kind,
    before: null,
  });
  const screenBefore = screens.now !== route.kind ? screens.now : screens.before;
  if (screens.now !== route.kind) setScreens({ now: route.kind, before: screens.now });
  // A chat that opens from the start page: its field comes down from under the greeting, as in the design. From
  // Sinopia, settings or any other screen the field stands at the bottom from the first frame.
  const [arrival, setArrival] = useState<{ id: string | null; fromHome: boolean }>({
    id: stageId,
    fromHome: false,
  });
  if (arrival.id !== stageId)
    setArrival({ id: stageId, fromHome: arrival.id === null && screenBefore === ROUTE.home });
  const record = useConversation(stageId && isConvId(stageId) ? stageId : null);
  const feed = useFeed(stageId && isConvId(stageId) ? stageId : null);
  const chatMachine = record?.machineId ?? feed?.machineId ?? null;
  // An open design has the right pane of its chat, as a chat has: the chat it came from, else its last one here.
  // An app opened as the main thing stands in Sinopia's stage: its chat and its pane are found the same way.
  let designOn: string | null = null;
  if (route.kind === ROUTE.site) designOn = route.id;
  else if (route.kind === ROUTE.app) designOn = appStageDesign(route.id, route.object ?? null);
  const designBound = useDesignBound(designOn && model.active ? designKey(model.active, designOn) : null);
  const designChat =
    route.kind === ROUTE.site || route.kind === ROUTE.app ? (route.chat ?? designBound) : null;
  const designRecord = useConversation(designChat && isConvId(designChat) ? designChat : null);
  // A design lives on one machine: its address names it (or its chat does). Opened after a reload while another
  // machine is active, the shell goes to the design's machine by itself, once for that design, as the person had to.
  const siteMachine = route.kind === ROUTE.site ? (route.machine ?? designRecord?.machineId ?? null) : null;
  const siteMachineKnown =
    siteMachine !== null && model.startMachines.some((m) => m.id === siteMachine && !m.off);
  const followedSite = useRef<string | null>(null);
  const { pickMachine: pickSiteMachine } = model;
  useEffect(() => {
    if (route.kind !== ROUTE.site || !siteMachineKnown || siteMachine === null) return;
    const key = `${route.id}|${siteMachine}`;
    if (followedSite.current === key) return;
    followedSite.current = key;
    if (siteMachine !== model.active) void pickSiteMachine(siteMachine);
  }, [route, siteMachine, siteMachineKnown, model.active, pickSiteMachine]);
  // A chat's terminal on its page, and on an open design the terminal of the design's chat.
  // A design's chat keeps its terminal in its own column, under its field: the design's view gives the slot.
  const [termSlot, setTermSlot] = useState<HTMLElement | null>(null);
  const term = useAgentTerminal(route.kind === ROUTE.chat ? (record ?? null) : (designRecord ?? null));
  const bg = useChatBackground(stageId && isConvId(stageId) ? stageId : null, chatMachine, now, openChat);
  const present = stageId !== null && chatMachine !== null;
  // The last chats opened stay mounted, hidden (<Activity>): back to one of them, its feed and field are already drawn
  // and only show again.
  const [keptChats, setKeptChats] = useState<readonly IKeptChat[]>([]);
  const keptSeen = keptChats.reduce((most, kept) => Math.max(most, kept.seen), 0);
  const onStage =
    route.kind === ROUTE.chat && queuedOpen === null && stageId !== null && isConvId(stageId) && chatMachine
      ? { id: stageId, machine: chatMachine }
      : null;
  if (onStage) {
    const was = keptChats.find((kept) => kept.id === onStage.id);
    const last = was !== undefined && was.seen === keptSeen;
    if (!last || was.machine !== onStage.machine)
      setKeptChats(keepChat(keptChats, onStage, last ? keptSeen : keptSeen + 1));
  }
  // What a kept chat calls, read when called: a new function each shell render would draw every hidden chat again.
  const chatCalls = useRef<IChatCalls | null>(null);

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
  const chatMissing = missing === currentId && !present && queuedOpen === null;
  useEffect(() => {
    if (!currentId || present || !listed) return;
    const timer = setTimeout(() => setMissing(currentId), CHAT_WAIT_MS);
    return () => clearTimeout(timer);
  }, [currentId, present, listed]);

  // Reading the chat reads it: what the agent finished while it is on screen is seen, beside an app's page too.
  const besideRoute = route.kind === ROUTE.app || route.kind === ROUTE.review;
  let besideId: string | null = null;
  if (route.kind === ROUTE.app) besideId = designChat;
  else if (route.kind === ROUTE.review) besideId = route.chat ?? null;
  const beside = useConversation(besideId && isConvId(besideId) ? besideId : null);
  useReadWhileVisible((besideRoute ? beside : record) ?? null);

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
    if (!drawerOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setDrawerOpen(false);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [drawerOpen]);
  const showTests = useShowTestChats();
  const shownChats = useMemo(
    () => (model.chats === null ? null : withoutTestChats(model.chats, showTests, currentId)),
    [model.chats, showTests, currentId],
  );
  const [navigatorOpen, setNavigatorOpen] = useState(false);
  const keysHelp = useShellKeys({
    chats: shownChats,
    currentId,
    navigate,
    openChat,
    toggleSearch: () => setSearchOpen((current) => !current),
    toggleRail,
    toggleNavigator: () => setNavigatorOpen((current) => !current && currentId !== null),
  });

  // The chat in sight, the open one or the one beside an app's page: its replies are no news.
  const notices = useNotices({
    person: owner || 'local',
    currentId: besideId ?? currentId,
    onOpenChat: openChat,
  });
  useTurnSummaries();
  setHistoryPerson(owner || 'local');

  // The machines whose agents may change the person's look (features/looks/agent-looks.tsx): the open one and the
  // open chat's.
  const lookMachines = useMemo(() => {
    const out = new Set<string>();
    if (model.active) out.add(model.active);
    return [...out];
  }, [model.active]);
  const folderOfChat = useCallback(
    (id: string) => model.chats?.find((item) => item.id === id)?.projectId ?? null,
    [model.chats],
  );
  const chat = useMemo(
    () => (currentId === null ? null : (model.chats?.find((item) => item.id === currentId) ?? null)),
    [currentId, model.chats],
  );
  const parentChat = useMemo(
    () => (chat?.parentId ? (model.chats?.find((item) => item.id === chat.parentId) ?? null) : null),
    [chat, model.chats],
  );
  const center = useNotificationCenter({
    person: owner || 'local',
    chats: model.chats,
    machineNotices: model.machineNotices,
    onOpenChat: (id) => {
      setNotifications(null);
      openChat(id);
    },
  });
  const notificationItems = center.items;
  const [machineReveal, setMachineReveal] = useState(0);
  const unread = notificationItems?.some((item) => item.unread) ?? false;
  // The apps of the search: read once the dialog is open, so a page that never searches does not ask.
  const searchAppsRead = useInstalledApps(model.active ?? 'local', 0, searchOpen && model.active !== null);
  const searchApps = searchAppsRead.list;
  // The apps did not come: their tab says it has none, it does not wait for ever.
  const searchAppsOff = searchAppsRead.error !== null || model.active === null;
  const searchChat = useConversation(currentId !== null && isConvId(currentId) ? currentId : null);
  const appsGroup = useMemo(
    () =>
      searchApps === null && !searchAppsOff
        ? undefined
        : appSearchGroup(
            searchApps?.apps ?? [],
            searchChat && currentId ? { id: currentId, apps: searchChat.apps ?? [] } : null,
            query,
            scope === SCOPE.apps,
          ),
    [searchApps, searchAppsOff, searchChat, currentId, query, scope],
  );
  const searchBeside = route.kind === ROUTE.chat && currentId !== null && isConvId(currentId);
  const smartSearch = useSmartSearch({
    machine: model.active,
    open: searchOpen,
    query,
    scope,
    chats: shownChats,
    appsGroup,
    now,
  });

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
  const hubItem: readonly IMoreItem[] =
    hubFirst && hubEntry
      ? [
          {
            id: 'to-hub',
            icon: 'layout-dashboard',
            label: t('next.sidebar.hub'),
            current: false,
            onSelect: () => window.location.assign(hubEntry),
          },
        ]
      : [];
  const profile: readonly IMoreItem[] = [
    ...hubItem,
    {
      id: 'settings',
      icon: 'settings',
      label: t('next.shell.settings'),
      current: route.kind === ROUTE.settings,
      onSelect: () => openScreen({ kind: ROUTE.settings, section: null }),
    },
  ];
  const moreAll: readonly IMoreItem[] = [
    {
      id: 'tasks',
      icon: 'list-checks',
      label: t('next.sidebar.tasks'),
      current: route.kind === ROUTE.tasks,
      onSelect: () => openScreen({ kind: ROUTE.tasks }),
    },
    {
      id: 'manage',
      icon: 'sliders-horizontal',
      label: t('next.sidebar.manage'),
      current: route.kind === ROUTE.manage || route.kind === ROUTE.stats || route.kind === ROUTE.efficiency,
      onSelect: () => openScreen({ kind: ROUTE.manage }),
    },
    {
      id: 'learn',
      icon: 'graduation-cap',
      label: t('next.learn.title'),
      current: route.kind === ROUTE.learn,
      onSelect: () => openScreen({ kind: ROUTE.learn }),
    },
    {
      id: 'help',
      icon: 'book-open',
      label: t('next.sidebar.help'),
      current: route.kind === ROUTE.help,
      onSelect: () => openScreen({ kind: ROUTE.help, section: null, page: null }),
    },
  ];
  // «Hub first»: no Tasks and Help; Management is the hub's row (and only when this person has a hub).
  const moreBase: readonly IMoreItem[] = hubFirst
    ? hubEntry
      ? [{ ...moreAll[1]!, onSelect: () => window.location.assign(hubEntry), current: false }]
      : []
    : moreAll;
  // test chats stay out of the list; the item that shows them is there while there are any (or they are shown).
  const testItem: readonly IMoreItem[] =
    showTests || hasTestChats(model.chats)
      ? [
          {
            id: 'test-chats',
            icon: showTests ? 'eye-off' : 'test-tube-diagonal',
            label: showTests ? t('next.sidebar.testChatsHide') : t('next.sidebar.testChatsShow'),
            current: false,
            onSelect: toggleShowTestChats,
          },
        ]
      : [];
  const more: readonly IMoreItem[] = [...moreBase, ...testItem];

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
  // The pane stays what it was while a page without one is on screen (settings, home, a list): it waits hidden with
  // its frames (a board of Sinopia, a browser) and comes back at once, not opened anew, which met a frame whose
  // address had gone stale («hub refused to connect»).
  const paneLive = route.kind === ROUTE.chat || route.kind === ROUTE.site || route.kind === ROUTE.app;
  const paneNow = {
    conv: paneConvOf(route, record ?? null, designRecord ?? null),
    beside: route.kind === ROUTE.site || route.kind === ROUTE.app,
    design: route.kind === ROUTE.site ? route.id : null,
    view: paneViewOf(route),
  };
  const [paneHeld, setPaneHeld] = useState(paneNow);
  if (
    paneLive &&
    (paneHeld.conv !== paneNow.conv ||
      paneHeld.beside !== paneNow.beside ||
      paneHeld.design !== paneNow.design ||
      paneHeld.view !== paneNow.view)
  )
    setPaneHeld(paneNow);
  const paneArgs = paneLive ? paneNow : paneHeld;
  const pane = useRightPane(
    paneArgs.conv,
    machineName,
    term.reveal,
    paneStrip,
    paneArgs.beside,
    paneArgs.design,
    paneArgs.view,
  );
  useLayoutEffect(() => {
    chatCalls.current = {
      openFile: pane.openFile,
      openOutput: pane.openOutput,
      openRef: pane.openRef,
      showBackground: bg.show,
      openTerminal: term.reveal,
    };
  });
  // A full-screen design has no chat page around it: its chat opens links, files and cards in the pane beside it.
  const designOpeners = {
    onOpenRef: pane.openRef,
    onOpenFile: pane.openFile,
    onOpenOutput: pane.openOutput,
  };
  const projectDialog = useProjectDialog(model.active, model.personSidebar);
  const projectSettings = useProjectSettingsDialog(model.active, model.personSidebar);
  const resumeDialog = useResumeDialog(model.active, machineName, now, (id) =>
    navigate({ kind: ROUTE.chat, id }),
  );
  // «/resume» in the field opens the same list of earlier sessions as the start page's link.
  useUiRequest(UI_REQUEST.resume, resumeDialog.show);
  const newChatProject = useNewChatProject();
  // the machine the next chat starts on, the one picked for it in the pill; a pick of a machine that cannot
  // take a chat now is no pick. HRN-1036: else not the machine on top as such, but the first of the person's that
  // answers now and has the agent of the last choice; with none answering, one whose agents the hub remembers, so the
  // pill and the limits are drawn all the same.
  const newChatMachine = useNewChatMachine();
  const [lastProvider] = useState(() => readLastChoice()?.provider ?? null);
  const startOn = startMachineOf(model.startCandidates, newChatMachine, lastProvider) ?? model.active;
  // none of the person's machines answers now: a new chat waits in the hub for its machine.
  const noneAnswers = model.startCandidates.length > 0 && model.startCandidates.every((c) => !c.answers);
  // The start page's own pane: the tools that need no chat (the machine's, the services, review).
  const homePane = useHomePane({
    machine: startOn,
    machines: model.startMachines,
    conversations: model.conversations,
    enabled: boot.k !== 'failed' && !noV2 && route.kind === ROUTE.home,
  });
  // On the phone a swipe from the chat's right side towards the middle brings the right pane up.
  const paneHeader = route.kind === ROUTE.chat ? pane.header : null;
  usePaneEdgeSwipe(phone && paneHeader && !paneHeader.open ? paneHeader.onToggle : null);
  // A view of an app that takes the start page's stage under the field: the page of the day it brings.
  const homeStage = useHomeStage(startOn);
  // Not one machine can take a new chat: the field says so instead of a pill with nothing in it.
  const allMachinesOff = model.startMachines.length > 0 && model.startMachines.every((m) => m.off);
  // Picked in the model's popover, for a person with several machines; the default one is no pick.
  const startPick = useMemo(
    () =>
      startOn && model.startMachines.length > 1
        ? {
            current: startOn,
            choices: model.startMachines,
            // A pick is the person's word, the machine on top too: it stays even while that machine does not answer.
            onPick: (id: string) => newChatMachineStore.set(id),
          }
        : null,
    [startOn, model.startMachines],
  );
  // Sinopia Design makes a design on the open machine (its library is that machine's): picking another one in the
  // create screen's model popover opens that machine, so the new design shows up where the person is.
  // The designs at work under «Sinopia Design»: read again whenever the person moves between screens.
  const workingDesigns = useWorkingDesigns(
    noV2 ? null : model.active,
    model.conversations,
    route.kind === ROUTE.site ? route.id : null,
    route.kind,
  );
  // ChatGPT's plain chats under their own item: it shows once an account is signed in or a chat is kept.
  const gptChats = useGptChats(noV2 ? null : model.active);
  const gptMachine = noV2 ? null : model.active;
  const gptOn = useGptSignedIn(gptMachine);
  const sidebarGpt =
    gptMachine && (gptOn || (gptChats !== null && gptChats.length > 0))
      ? {
          rows: gptChats ?? [],
          current: route.kind === ROUTE.chatgpt ? route.id : null,
          onOpen: (id: string) => {
            navigate({ kind: ROUTE.chatgpt, id });
            setDrawerOpen(false);
          },
          onRename: (id: string, title: string) => renameGptChat(gptMachine, id, title).then(gptChatsChanged),
          onDelete: (id: string) =>
            deleteGptChat(gptMachine, id).then(() => {
              gptChatsChanged();
              if (route.kind === ROUTE.chatgpt && route.id === id) navigate({ kind: ROUTE.home });
            }),
        }
      : null;
  const openAppScreen = useCallback(
    (to: TRoute) => {
      navigate(to);
      setDrawerOpen(false);
    },
    [navigate],
  );
  const sidebarApps = useSidebarApps(noV2 ? null : model.active, model.conversations, route, openAppScreen);
  const designMachine = useMemo(
    () =>
      model.active && model.startMachines.length > 1
        ? {
            current: model.active,
            choices: model.startMachines,
            onPick: (id: string) => void pickMachine(id),
          }
        : null,
    [model.active, model.startMachines, pickMachine],
  );
  // A project picked for the next chat that the list no longer has is no pick.
  const newChatInto = model.projects?.find((project) => project.id === newChatProject) ?? null;
  const newChatName = newChatInto?.name ?? null;
  const into = useMemo(
    () => (newChatName === null ? null : { name: newChatName, onClear: () => newChatProjectStore.set(null) }),
    [newChatName],
  );
  // The sidebar's actions read the chats when they run, not when they were made: a frame of any chat made a
  // new list, the actions anew, and every row of the sidebar drew again.
  const chatsNow = useRef(model.chats);
  useLayoutEffect(() => {
    chatsNow.current = model.chats;
  });
  const chatActions = {
    ...model.actions,
    onOpenInNewTab: tabs.enabled ? (id: string) => openInNewTab({ kind: ROUTE.chat, id }, true) : undefined,
    onNewProject: projectDialog.show,
    onProjectSettings: projectSettings.show,
    onAttachChat:
      currentId !== null && isConvId(currentId)
        ? (id: string) => {
            const chat = chatsNow.current?.find((item) => item.id === id);
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
      hubHref={hubEntry}
      chats={noV2 ? NO_CHATS : shownChats}
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
        newChatMachineStore.set(null);
        resetHome();
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
      apps={sidebarApps}
      nav={me.k === 'ready' ? me.me.settings.nav : undefined}
      onNav={saveNav}
      gpt={sidebarGpt}
      designs={
        workingDesigns && {
          rows: workingDesigns,
          current: route.kind === ROUTE.site ? route.id : null,
          onOpen: (id) => {
            navigate({ kind: ROUTE.site, id });
            setDrawerOpen(false);
          },
        }
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
  const manageTool: IHomeTool =
    hubFirst && hubEntry
      ? {
          id: 'manage',
          icon: 'sliders-horizontal',
          label: t('next.sidebar.manage'),
          onOpen: (newTab) =>
            newTab ? void window.open(hubEntry, '_blank', 'noopener') : window.location.assign(hubEntry),
        }
      : toolOf('manage', 'sliders-horizontal', t('next.sidebar.manage'), { kind: ROUTE.manage });
  const homeTools: readonly IHomeTool[] = [
    toolOf('designly', 'palette', t('next.sidebar.designly'), { kind: ROUTE.designly }),
    ...(hubFirst ? [] : [toolOf('tasks', 'list-checks', t('next.sidebar.tasks'), { kind: ROUTE.tasks })]),
    {
      id: 'search',
      icon: 'search',
      label: t('next.sidebar.search'),
      onOpen: () => setSearchOpen(true),
    },
    ...(hubFirst && !hubEntry ? [] : [manageTool]),
    toolOf('settings', 'settings', t('next.shell.settings'), { kind: ROUTE.settings, section: null }),
  ];

  const overlays = (
    <>
      <KeptAppFrames />
      {projectDialog.dialog}
      {projectSettings.dialog}
      <AppConfirmHost />
      <AppFilePickHost />
      {resumeDialog.dialog}
      <NotificationCenter
        open={notifications !== null}
        anchor={notifications?.anchor ?? null}
        side={notifications?.side ?? 'right'}
        items={notificationItems}
        failed={listFailed}
        now={now}
        onClose={() => setNotifications(null)}
        onRead={center.onRead}
        onRemove={center.onRemove}
        onReadAll={center.onReadAll}
        renderWait={center.renderWait}
        onOpenItem={(item) => {
          readOpened(item);
          setNotifications(null);
          if (item.chatId) openChat(item.chatId);
          else setMachineReveal((n) => n + 1);
        }}
      />
      {navigatorOpen && currentId && isConvId(currentId) && (
        <Suspense fallback={null}>
          <ChatNavigator open conv={currentId} onClose={() => setNavigatorOpen(false)} />
        </Suspense>
      )}
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
            scopes: smartSearch.scopes,
            scope,
            onScope: setScope,
            results: smartSearch.results,
            failed: listFailed,
            indexing: smartSearch.indexing,
            recent: NO_ROWS,
            idle: smartSearch.idle,
            empty: smartSearch.empty,
            marks: smartSearch.marks,
            smart: {
              answer: smartSearch.answer,
              onAsk: smartSearch.onAsk,
              onSetting: smartSearch.onSetting,
              detail: smartSearch.detail,
              kept: smartSearch.kept,
              beside: searchBeside,
              onAside: (row: ISearchRow) => {
                const link = row.go?.k === ROW_GO.link || row.go?.k === ROW_GO.review ? row.go.url : null;
                // A link opens beside the chat on screen; without one (or for a chat, a design) in a new tab.
                if (link !== null && searchBeside) {
                  setSearchOpen(false);
                  pane.openRef(link);
                  return;
                }
                const address =
                  link ?? smartSearch.kept.get(row.id)?.link ?? smartSearch.detail.now(row).link;
                if (address) window.open(address, '_blank', 'noopener');
              },
              onChatAct: async (chatId: string, what) => {
                if (model.active === null || !isConvId(chatId)) return false;
                const done = await (
                  what === 'stop' ? interrupt(model.active, chatId) : resumeConversation(model.active, chatId)
                ).done;
                if (done.k === 'done') return true;
                toast(t('next.search.chatActFailed'), { tone: 'error' });
                return false;
              },
              onAddSource: () => {
                setSearchOpen(false);
                openScreen({ kind: ROUTE.settings, section: 'connections' });
              },
              onPickModel: () => {
                setSearchOpen(false);
                openScreen({ kind: ROUTE.settings, section: 'smart' });
              },
            },
            onPick: (row: ISearchRow) => {
              setSearchOpen(false);
              setQuery('');
              setScope(SCOPE.all);
              if (row.go) {
                const go = row.go;
                if (go.k === ROW_GO.setting || go.k === ROW_GO.section)
                  openScreen({ kind: ROUTE.settings, section: go.section });
                else if (go.k === ROW_GO.design) openScreen({ kind: ROUTE.site, id: go.id });
                else if (go.k === ROW_GO.review)
                  openScreen({ kind: ROUTE.review, url: go.url, ...(currentId ? { chat: currentId } : {}) });
                else if (go.k === ROW_GO.link) window.open(go.url, '_blank', 'noopener');
                else if (go.k === ROW_GO.designs) openScreen({ kind: ROUTE.designly });
                else if (go.k === ROW_GO.chat) openChat(go.id);
                else {
                  newChatProjectStore.set(null);
                  newChatMachineStore.set(null);
                  resetHome();
                  openScreen(HOME);
                }
              } else if (row.id.startsWith(APP_ROW.command)) {
                const [chatId, ...words] = row.id.slice(APP_ROW.command.length).split('|');
                if (chatId && isConvId(chatId)) {
                  setDraftText(chatId, words.join('|'));
                  openChat(chatId);
                }
              } else if (row.id.startsWith(APP_ROW.open)) {
                const id = row.id.slice(APP_ROW.open.length);
                navigate({
                  kind: ROUTE.app,
                  id,
                  ...(currentId && !isSpaceApp(id) ? { chat: currentId } : {}),
                });
              } else if (row.id.startsWith(APP_ROW.pane)) openAppTab(row.id.slice(APP_ROW.pane.length));
              else if (row.target && isConvId(row.target.chatId)) {
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
      <SmartNoticeHost
        person={owner || 'local'}
        notices={notices.notices}
        machineNotices={model.machineNotices}
        machine={boot.k === 'ready' ? model.active : null}
        currentId={besideId ?? currentId}
        onOpenChat={openChat}
        onOpenMachine={() => setMachineReveal((n) => n + 1)}
        onOpenSettings={() => openScreen({ kind: ROUTE.settings, section: 'notifications' })}
        quiet={noticeQuiet(railModeOf(route) === RAIL_MODE.full, phone && drawerOpen)}
        onOpenCenter={() => {
          // The bell over the chat, or the one of the side menu when no chat is open.
          const header = document.querySelector<HTMLElement>('.hx-header__bell');
          const side = document.querySelector<HTMLElement>(
            `[aria-label="${t('next.sidebar.notifications')}"]`,
          );
          if (header) toggleNotifications(header, 'bottom');
          else if (side) toggleNotifications(side, 'right');
        }}
      />
      <BrowserProfileAskHost machine={boot.k === 'ready' ? model.active : null} />
      <BrowserLoginAskHost
        machine={boot.k === 'ready' ? model.active : null}
        conv={currentId}
        onOpen={openChat}
      />
      <SecretAskHost machine={boot.k === 'ready' ? model.active : null} />
      <ToastHost />
      <RolloutBanner />
      <UpdateBanner available={update.available} onReload={update.reload} onDismiss={update.dismiss} />
      <ReleaseNotice release={update.available ? null : release.release} onDismiss={release.dismiss} />
      <WhatsNew ready={boot.k === 'ready'} updating={update.available} />
      <LearnWelcome fresh={boot.k === 'ready' && model.chats !== null && model.chats.length === 0} />
      <TourOverlay />
      <VoiceLayer />
      <Suspense fallback={null}>
        <VoiceFull
          titleOf={(id) => {
            const gpt = gptIdOf(id);
            if (gpt !== null) return gptChats?.find((c) => c.id === gpt)?.title ?? null;
            return model.conversations?.find((c) => c.id === id)?.title ?? null;
          }}
        />
        <VoiceBar
          titleOf={(id) => {
            const gpt = gptIdOf(id);
            if (gpt !== null) return gptChats?.find((c) => c.id === gpt)?.title ?? null;
            return model.conversations?.find((c) => c.id === id)?.title ?? null;
          }}
          onBack={(id) => {
            const gpt = gptIdOf(id);
            navigate(gpt === null ? { kind: ROUTE.chat, id } : { kind: ROUTE.chatgpt, id: gpt });
          }}
          at={JSON.stringify(route)}
        />
      </Suspense>
      {home && (
        <Suspense fallback={null}>
          <WakeWord machine={home} onWake={startByWake} />
        </Suspense>
      )}
      {voiceFloat && (
        <Suspense fallback={null}>
          <VoiceFloat
            machine={voiceFloat.machine}
            conv={voiceFloat.conv}
            onClose={() => setVoiceFloat(null)}
          />
        </Suspense>
      )}
      {keysHelp}
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

  const { chatShare } = useBeta();
  const headerShare = chatShare && record ? { machine: record.machineId, tabId: record.id } : null;

  const headerMachine = {
    card: model.machine,
    // Every other machine of the person, also where the chats of all machines are one list: the header's
    // card is where a machine is picked and its state seen, so it lists them all with their health.
    others: model.startMachines.filter((m) => m.id !== model.machine?.id),
    onPick: pickMachine,
    onManage: () => openScreen({ kind: ROUTE.settings, section: 'machines' }),
    reveal: machineReveal,
  };

  return (
    <RailFrame
      drawerOpen={drawerOpen}
      onDrawerChange={setDrawerOpen}
      sidebar={sidebar}
      tabs={tabs.strip}
      aside={
        <>
          <Activity mode={boot.k !== 'failed' && !noV2 && paneLive ? 'visible' : 'hidden'}>
            {pane.drawer}
          </Activity>
          {!(boot.k !== 'failed' && !noV2 && paneLive) && (homePane.drawer || null)}
        </>
      }
      overlays={overlays}
      textSize={me.k === 'ready' ? me.me.settings.textSize : undefined}
    >
      {/* The open design, the Apps screen and Settings have their own strip at the top: no header over them. */}
      {route.kind !== ROUTE.site &&
        route.kind !== ROUTE.invite &&
        route.kind !== ROUTE.tasks &&
        route.kind !== ROUTE.apps &&
        route.kind !== ROUTE.help &&
        route.kind !== ROUTE.learn &&
        route.kind !== ROUTE.chatgpt &&
        route.kind !== ROUTE.app &&
        route.kind !== ROUTE.review &&
        route.kind !== ROUTE.appAbout &&
        route.kind !== ROUTE.appDoc &&
        route.kind !== ROUTE.kit &&
        route.kind !== ROUTE.settings && (
          <LoadedChatHeader
            shown={model.shown}
            chat={chat}
            projects={model.projects}
            actions={model.actions}
            machine={headerMachine}
            drawerOpen={drawerOpen}
            onOpenDrawer={() => setDrawerOpen(true)}
            onNewChat={() => {
              resetHome();
              navigate(HOME);
            }}
            pane={pane.header ?? homePane.header}
            terminal={term.header}
            background={bg.header}
            goal={headerGoal}
            share={headerShare}
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
            chats={{ list: shownChats, onOpen: openChat }}
          />
        )}
      {/* The chat read to its end and pulled further: the next active chat, as in Telegram (HRN-1491). */}
      <NextChatPull
        enabled={phone && route.kind === ROUTE.chat}
        chats={shownChats}
        currentId={currentId}
        contextOf={(id) => {
          const conv = conversationsStore.get(id as ConvId);
          return conv?.recap?.text ?? conv?.lastTurn?.snippet ?? null;
        }}
        onOpen={openChat}
      />
      {(boot.k === 'failed' || noV2) && <BootNotice kind={boot.k === 'failed' ? 'failed' : 'no-v2'} />}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.home && (
        <Greeting
          name={name}
          stage={homeStage.stage}
          greeting={homeStage.greeting}
          fromChat={screens.before === ROUTE.chat}
          onResume={resumeDialog.show}
          into={into}
          setup={
            <MachineSetup
              machine={startOn}
              onOpenSafe={() => openScreen({ kind: ROUTE.settings, section: 'safe' })}
            />
          }
          composer={(hello) => (
            <HomeComposer
              above={hello}
              machine={startOn}
              conversations={model.conversations}
              projectId={newChatInto?.id ?? null}
              resolveProject={model.personSidebar ? machineProjectOf : undefined}
              machines={startPick}
              allMachinesOff={allMachinesOff}
              queueWhenSilent={noneAnswers}
              onQueued={(id) => {
                newChatProjectStore.set(null);
                newChatMachineStore.set(null);
                goTo({ kind: ROUTE.chat, id: queuedChatId(id) });
              }}
              below={
                <>
                  <HomeTools tools={homeTools} />
                  <HomeSlots machine={startOn} />
                </>
              }
              onCreated={(id) => {
                newChatProjectStore.set(null);
                newChatMachineStore.set(null);
                goTo({ kind: ROUTE.chat, id });
              }}
            />
          )}
        />
      )}
      <LookLayer chatId={currentId} folderId={chat?.projectId ?? null} />
      <LookSheet />
      {lookMachines.map((machine) => (
        <AgentLooks key={machine} machine={machine} folderOf={folderOfChat} />
      ))}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat && (
        <ChatStage
          chatKey={stageId}
          leaving={leaving}
          fromHome={arrival.fromHome}
          side={bg.panel}
          paneOpen={pane.open}
          missing={chatMissing ? { onHome: () => goTo(HOME) } : null}
        >
          {queuedOpen !== null && <QueuedChatScreen itemId={queuedOpen} />}
          {keptChats.map((kept) => (
            <Activity key={kept.id} mode={kept.id === onStage?.id ? 'visible' : 'hidden'}>
              <KeptChat conv={kept.id} machine={kept.machine} calls={chatCalls} />
            </Activity>
          ))}
          {queuedOpen === null && !onStage && !chatMissing && <QuietChat />}
        </ChatStage>
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat && term.panel}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chat && pane.sideFloat}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.designly && model.active && (
        <DesignlyView key={model.active} machine={model.active} machines={designMachine} />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.designlyNew && model.active && (
        <DesignlyView
          key={model.active}
          machine={model.active}
          machines={designMachine}
          initialKind={DESIGN_KIND.slides}
        />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.videoNew && model.active && (
        <DesignlyView
          key={model.active}
          machine={model.active}
          machines={designMachine}
          initialKind={DESIGN_KIND.video}
        />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.tasks && !hubFirst && model.active && (
        <AppsScreen key={model.active} machine={model.active} onOpenDrawer={() => setDrawerOpen(true)} />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.apps && model.active && (
        <AppsListPage key={model.active} machine={model.active} onOpenDrawer={() => setDrawerOpen(true)} />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.help && !hubFirst && model.active && (
        <HelpPage
          key={model.active}
          machine={model.active}
          section={route.section}
          page={route.page}
          anchor={route.anchor ?? null}
          onOpenDrawer={() => setDrawerOpen(true)}
        />
      )}
      {route.kind === ROUTE.learn && <LearnPage onOpenDrawer={() => setDrawerOpen(true)} />}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.chatgpt && (route.machine ?? model.active) && (
        <ChatGptScreen
          key={`${route.machine ?? model.active}/${route.id}`}
          machine={(route.machine ?? model.active) as MachineId}
          id={route.id}
          onOpenSettings={() => navigate({ kind: ROUTE.settings, section: 'accounts' })}
          onOpenDrawer={() => setDrawerOpen(true)}
        />
      )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.appAbout && model.active && (
        <AppAboutPage
          key={`${model.active}/${route.id}`}
          machine={model.active}
          id={route.id}
          onOpenDrawer={() => setDrawerOpen(true)}
        />
      )}
      {model.active && <BrandingHost machine={model.active} />}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.settings && model.active && (
        <SettingsScreen
          key={model.active}
          machine={model.active}
          section={route.section}
          connect={route.connect}
          name={name ?? login}
        />
      )}
      {!hubFirst && route.kind === ROUTE.manage && <ManageScreen onOpen={navigate} />}
      {!hubFirst && route.kind === ROUTE.stats && (
        <LegacyScreen screen={LEGACY_SCREEN.stats} onOpen={navigate} />
      )}
      {!hubFirst && route.kind === ROUTE.efficiency && (
        <LegacyScreen screen={LEGACY_SCREEN.efficiency} onOpen={navigate} />
      )}
      {route.kind === ROUTE.invite && <InviteAccept key={route.token} token={route.token} />}
      {boot.k !== 'failed' &&
        !noV2 &&
        route.kind === ROUTE.review &&
        route.url === undefined &&
        model.active && (
          <ReviewAppPage
            key={`/${route.chat ?? ''}/${tabs.activeId}`}
            machine={model.active}
            url={null}
            chat={route.chat ?? null}
            wt={route.wt ?? null}
            open={designOpeners}
            toolPane={pane.header}
            onOpenDrawer={() => setDrawerOpen(true)}
          />
        )}
      {boot.k !== 'failed' && !noV2 && route.kind === ROUTE.site && !model.active && <SiteWait />}
      {boot.k !== 'failed' && !noV2 && model.active && (
        <KeptScreens
          machine={model.active}
          route={route}
          activeId={tabs.activeId}
          terminal={term.header}
          onTermSlot={setTermSlot}
          toolPane={pane.header}
          open={designOpeners}
          onOpenDrawer={() => setDrawerOpen(true)}
        />
      )}
      {boot.k !== 'failed' &&
        !noV2 &&
        route.kind === ROUTE.site &&
        termSlot &&
        createPortal(term.panel, termSlot)}
      <RailFollowsPane />
    </RailFrame>
  );
};

export default Shell;
