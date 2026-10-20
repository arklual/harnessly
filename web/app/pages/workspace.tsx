import { appConfig, savedLayoutOf } from '@/platform/config';
import { trace } from '@/lib/hl-ui-trace';
import useLastingErrors from '@/hooks/use-lasting-errors';
import Head from 'next/head';
import dynamic from 'next/dynamic';
import { Activity, useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react';
import type { TNextPageWithLayout } from '@/pages/_app';
import { useRouter } from 'next/router';
import { useTheme } from 'next-themes';
import { SWRConfig } from 'swr';
import { AnimatePresence, motion } from 'motion/react';
import usePaneColumnFlip from '@/hooks/use-pane-column-flip';
import { loadWorkspaces, openAgentTab } from '@/lib/hl-open-agent';
import {
  chatConversationKey,
  withConversationMachine,
  type Conversation,
  type TConversationRef,
} from '@/lib/hl-conversations';
import {
  CENTRE_VIEW,
  DESIGN_HOME,
  agentRefOfTab,
  conversationUrl,
  findConversationByRef,
  parseAppLocation,
  placeholderChat,
  wantedLocation,
  type IAppLocation,
  type IDesignLocation,
  type TCentreView,
} from '@/lib/hl-conversation-url';
import useConversationUrl from '@/hooks/use-conversation-url';
import type { IStartOptions, TNewConversationKind } from '@/lib/hl-new-conversation';
import { setPendingFirstCommand } from '@/lib/hl-pending-command';
import {
  UNASSIGNED_TAB,
  clearPendingLaunch,
  readPendingLaunch,
  setPendingLaunch,
} from '@/lib/hl-pending-launch';
import LaunchComposer from '@/components/agents/composer/launch-composer';
import { ensureLibraryPort } from '@/hooks/use-design-library';
import AppUpdateBanner from '@/components/agents/app-update-banner';
import {
  BASE_SHELL_SHORTCUTS,
  PANE_SUMMARY_MESSAGE,
  parseFrameView,
  shellShortcutFromEvent,
  type IShellShortcut,
  type TShellShortcutId,
  type TSwipeDirection,
} from '@/lib/hl-frame-bridge';
import { OPEN_PANE_TAB_EVENT, isPaneTabRequest, paneShortcutFromEvent } from '@/lib/hl-pane-tabs';
import { buildHandoffBrief } from '@/lib/hl-handoff';
import { requestHandoffNote } from '@/lib/hl-handoff-client';
import { browserLayoutStorage, readPaneLayout, savePaneLayout } from '@/lib/hl-pane-layout';
import { applyPaneRequest, openPaneTab, paneKeyOf, readPane } from '@/lib/hl-pane-store';
import { GET_PANE_STATE_EVENT, SET_PANE_VISIBLE_EVENT } from '@/lib/hl-pane-bus';
import { collectPanes, useLayoutStore } from '@/hooks/use-layout';
import useTabStore from '@/hooks/use-tab-store';
import { PANE_KINDS } from '@/components/agents/pane/pane-registry';
import usePaneAutoOpen from '@/hooks/use-pane-auto-open';
import usePaneIntents from '@/hooks/use-pane-intents';
import useAgentPanelCommands from '@/hooks/use-agent-panel-commands';
import { isConversationRef } from '@/lib/hl-organizer';
import { ROW_STATE, type IRowModel, type ISidebarMachine } from '@/lib/hl-sidebar-model';
import type { TThemeMode } from '@/components/agents/conversation-sidebar';
import { markAgentLaunching } from '@/hooks/use-agent-starting';
import useSync from '@/hooks/use-sync';
import useSpeedMarks from '@/hooks/use-speed-marks';
import { markConversationOpen, markOpenPhase, noteOpenScreen } from '@/lib/hl-speed-client';
import useIsMobile from '@/hooks/use-is-mobile';
import { useMineAlert } from '@/hooks/use-efficiency';
import useBackLayer from '@/hooks/use-back-layer';
import useTouchGestures from '@/hooks/use-touch-gestures';
import usePinnedViewport from '@/hooks/use-pinned-viewport';
import useViewportReport from '@/hooks/use-viewport-report';
import useHlConversations from '@/hooks/use-hl-conversations';
import { useMachineFeatures, useHlMachines } from '@/hooks/use-hl-machines';
import useSidebarView from '@/hooks/use-sidebar-view';
import useSidebarModel from '@/components/agents/sidebar/use-sidebar-model';
import useSelectionHistory from '@/components/agents/sidebar/use-selection-history';
import SidebarProvider from '@/components/agents/sidebar/sidebar-provider';
import { organizerActions } from '@/components/agents/sidebar/organizer-actions';
import { toast } from 'sonner';
import { initTerminalTheme } from '@/hooks/use-terminal-theme';
import type { IQuickPromptsData } from '@/lib/quick-prompts-store';
import type { ISidebarItemsData } from '@/lib/sidebar-items-store';
import type { IConfigInitialData } from '@/hooks/use-config-store';
import ConversationSidebar from '@/components/agents/conversation-sidebar';
import RuntimeWorkspace from '@/components/agents/runtime-workspace';
import MachineRuntimeFrame from '@/components/agents/machine-runtime-frame';
import { retainMachineFrames, visitMachineFrame, type TMachineFrames } from '@/lib/hl-machine-frames';
import OfflineConversation from '@/components/agents/offline-conversation';
import useMachineReach from '@/hooks/use-machine-reach';
import { useNetworkHealthStore } from '@/lib/network-health';
import { TerminalSurfaceVisibleContext } from '@/hooks/use-terminal-surface-visible';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import useRemoteMachineStatuses from '@/hooks/use-remote-machine-statuses';
import { homeFetch, onViewMachineChange, setViewMachine, viewMachineId } from '@/lib/app-path';
import { noFrameMachinesOf, readNoFrameMachines } from '@/lib/no-frame';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { jumpToEntry } from '@/lib/hl-timeline-jump';
import ConversationRail from '@/components/agents/conversation-rail';
import WorkspaceHeader from '@/components/agents/workspace-header';
import type { IHeaderSlotProps } from '@/components/agents/header/header-slot';
import WorkspaceWelcome from '@/components/agents/workspace-welcome';
import ConversationSkeleton from '@/components/agents/conversation-skeleton';
import useActiveAgentTab from '@/hooks/use-active-agent-tab';
import ColumnResizer from '@/components/agents/column-resizer';
import AgentNotifier from '@/components/agents/agent-notifier';
import { OPEN_CONVERSATION_EVENT } from '@/hooks/use-web-push';
import {
  boardAddressPort,
  OPEN_BOARD_EVENT,
  parseBoardOpenQuery,
  type IBoardOpenRequest,
} from '@/lib/hl-design-library/phone';
import { OPEN_DESIGN_EVENT } from '@/lib/hl-open-design';
import type { CSSProperties } from 'react';
import { CloudOff, ListTodo, PanelRight } from 'lucide-react';
import { locale, t } from '@/lib/i18n';

const FLEET_ERROR_AFTER_MS = 6_000;
const TerminalPage = dynamic(() => import('@/components/workspace/terminal-page'), { ssr: false });
const ChatView = dynamic(() => import('@/components/agents/chat/chat-view'), { ssr: false });
// The island stands at its width while its code loads: an empty host let the conversation fill the column and then
// narrow, reflowing every reply.
const ConversationPane = dynamic(() => import('@/components/agents/pane/conversation-pane'), {
  ssr: false,
  loading: () => <section className="tw-rpane" aria-hidden />,
});
const PaneSummary = dynamic(() => import('@/components/agents/pane/pane-summary'), { ssr: false });
const CommandPalette = dynamic(() => import('@/components/agents/command-palette'), { ssr: false });
const ConversationNavigator = dynamic(() => import('@/components/agents/conversation-navigator'), {
  ssr: false,
});
const StatsView = dynamic(() => import('@/components/agents/stats/stats-view'), { ssr: false });
const EfficiencyView = dynamic(() => import('@/components/agents/efficiency/efficiency-view'), {
  ssr: false,
});
const LibraryHome = dynamic(() => import('@/components/agents/design/library/library-home'), { ssr: false });
const LaunchPreview = dynamic(
  () => import('@/components/timeline/timeline-view').then((m) => m.LaunchPreview),
  { ssr: false },
);

interface IWorkspaceProps {
  owner: string;
  initialConfig: IConfigInitialData;
  initialQuickPrompts: IQuickPromptsData;
  initialSidebarItems: ISidebarItemsData;
}

// How long a link waits for its tab to appear in the list (a runtime has just started it).
const OPEN_WAIT_MS = 30_000;
/** A remote machine's frame takes a request for its panel only once it is ready; opening a board again is a no-op. */
const REMOTE_BOARD_RETRY_MS = [1_500, 4_000, 8_000] as const;

// A column grows beside the conversation; a drawer slides over it without reflowing the conversation every frame.
const COLUMN = {
  initial: { width: 0, opacity: 0 },
  animate: { width: 'auto', opacity: 1 },
  exit: { width: 0, opacity: 0 },
};
/** After the first screen has settled: the phone drawer is built then, off the path of any tap. */
const DRAWER_PREMOUNT_MS = 2500;
const INSPECTOR_KEY = 'hl:inspector';

/** A conversation a link names that the list does not have yet: the screen shows its skeleton until it appears. */
interface IPendingOpen {
  ref: TConversationRef;
  until: number;
}

interface IChatInstance {
  key: string;
  chatId: string | null;
  initialPrompt?: string;
  /** Organizer project the chat joins once its first message creates it. */
  pendingProjectId?: string;
}

const readInspectorOpen = (): boolean => {
  try {
    const saved = JSON.parse(localStorage.getItem(INSPECTOR_KEY) || 'null');
    return typeof saved?.open === 'boolean' ? saved.open : true;
  } catch {
    return true;
  }
};
const THEME_CYCLE = { system: 'light', light: 'dark', dark: 'system' } as const satisfies Record<
  TThemeMode,
  TThemeMode
>;
const HOTKEY_IDS = [
  '1',
  '2',
  '3',
  '4',
  '5',
  '6',
  '7',
  '8',
  '9',
] as const satisfies readonly TShellShortcutId[];
const LEFT_DEFAULT = 272;
const RIGHT_DEFAULT = 480;
const RIGHT_MIN = 360;
/** Widths saved for the island column (320 by default) are too narrow for a panel of tabs: they start over at the default. */
const paneWidthOf = (saved: number | undefined): number =>
  saved !== undefined && saved >= RIGHT_MIN ? saved : RIGHT_DEFAULT;
/** Narrowest the chat column gets when the right panel is dragged wide on a big monitor. */
const CENTER_MIN = 440;

const WorkspaceShell = ({
  owner,
  initialConfig,
  initialQuickPrompts,
  initialSidebarItems,
}: IWorkspaceProps) => {
  const savedLayout = savedLayoutOf<{ left?: number; right?: number; collapsed?: boolean }>(initialConfig);
  useSync();
  const router = useRouter();
  const isMobile = useIsMobile();
  /** Below this width the right panel covers the conversation instead of standing beside it. */
  const inspectorOverlay = useIsMobile(1100);
  const { resolvedTheme, theme: themeChoice, setTheme } = useTheme();
  const theme = resolvedTheme === 'light' ? 'light' : 'dark';
  const themeMode: TThemeMode = themeChoice === 'light' || themeChoice === 'dark' ? themeChoice : 'system';
  const data = useHlConversations();
  const { conversations, defaultMachineId, chatReady, setError, refresh, upsertChat } = data;
  useSpeedMarks({ owner, listReady: !data.loading, defaultMachineId });

  const [chosenMachineId, setChosenMachineId] = useState<string>();
  const machineId = chosenMachineId || defaultMachineId;
  const machineWorkspaces = useMemo(
    () => data.workspaces.filter((workspace) => !workspace.machineId || workspace.machineId === machineId),
    [data.workspaces, machineId],
  );
  const features = useMachineFeatures(machineId);
  const [remoteAgents, setRemoteAgents] = useState<TMachineFrames>({});
  const [frameEpochs, setFrameEpochs] = useState<Record<string, number>>({});
  // A machine late for the first fleet answer is not down: the banner and the rows wait for a lasting outage.
  const lastingFleetErrors = useLastingErrors(data.fleetErrors, FLEET_ERROR_AFTER_MS);
  // The machine whose conversation the shell draws itself instead of its frame (`no-frame`): its statuses,
  // layout and tabs are the ones in the shell's stores while it is on screen, and the home machine is watched like
  // any other then.
  const [viewMachine, setViewMachineState] = useState<string | undefined>(viewMachineId);
  useEffect(() => onViewMachineChange(setViewMachineState), []);
  useEffect(() => () => setViewMachine(undefined), []);
  const statusMachineId = viewMachine ?? defaultMachineId ?? undefined;
  const sidebar = useSidebarModel({
    conversations,
    fleetErrors: lastingFleetErrors,
    agentsLoaded: data.agentsLoaded,
    chatReady,
    defaultMachineId,
    statusMachineId,
  });
  const { model } = sidebar;
  const fleetMachines = useHlMachines().data?.machines;
  const [knownNoFrame] = useState(readNoFrameMachines);
  const noFrameMachines = useMemo(
    () => noFrameMachinesOf(fleetMachines, knownNoFrame),
    [fleetMachines, knownNoFrame],
  );
  // Without a fleet (a runtime opened directly) there are no other machines to watch.
  const remoteMachineIds = useMemo(
    () =>
      defaultMachineId
        ? (fleetMachines ?? [])
            .filter((m) => m.id !== statusMachineId && m.available !== false)
            .map((m) => m.id)
        : [],
    [fleetMachines, defaultMachineId, statusMachineId],
  );
  useRemoteMachineStatuses(remoteMachineIds);
  const organizerMachineName =
    fleetMachines?.find((m) => m.id === defaultMachineId)?.name ?? defaultMachineId ?? null;
  const [selected, setSelected] = useState<Conversation | null>(null);
  const [pending, setPending] = useState<IPendingOpen | null>(null);
  const [agentMounted, setAgentMounted] = useState(false);
  const [chatInstance, setChatInstance] = useState<IChatInstance | null>(null);
  const [sharedPath, setSharedPath] = useState('');
  const [inspectorOpen, setInspectorOpen] = useState(false);
  const [paneMaximised, setPaneMaximised] = useState(false);
  const [summaryOpen, setSummaryOpen] = useState(false);
  const summaryAnchor = useRef<HTMLButtonElement>(null);
  const activeTab = useActiveAgentTab();
  const [busy, setBusy] = useState(false);
  /** Puts a kind and a text into the welcome composer: a failed start brings its message back. */
  const [composeRequest, setComposeRequest] = useState<{
    kind: TNewConversationKind;
    seq: number;
    text?: string;
  } | null>(null);
  const [sidebarOpen, setSidebarOpen] = useState(() => !savedLayout?.collapsed);
  // On a phone the drawer stays mounted once built, so opening it is a CSS slide, not hundreds of rows
  // mounting mid-animation. It is built when the page has settled, or at once if it is opened before that.
  const [drawerMounted, setDrawerMounted] = useState(false);
  if (isMobile && sidebarOpen && !drawerMounted) setDrawerMounted(true);
  useEffect(() => {
    if (!isMobile || drawerMounted) return;
    const timer = window.setTimeout(() => setDrawerMounted(true), DRAWER_PREMOUNT_MS);
    return () => window.clearTimeout(timer);
  }, [isMobile, drawerMounted]);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteMode, setPaletteMode] = useState<'default' | 'search'>('default');
  const openSearchPalette = useCallback(() => {
    setPaletteMode('search');
    setPaletteOpen(true);
  }, []);
  const [columns, setColumns] = useState(() => ({
    left: savedLayout?.left ?? LEFT_DEFAULT,
    right: paneWidthOf(savedLayout?.right),
    collapsed: savedLayout?.collapsed ?? false,
  }));
  const commitColumn = useCallback((side: 'left' | 'right', width: number) => {
    setColumns((current) => {
      const next = { ...current, [side]: width };
      void homeFetch('/api/config', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ [appConfig().layoutKey]: next }),
      });
      return next;
    });
  }, []);
  useEffect(() => {
    if (isMobile) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the layout follows the sidebar and is saved to the server
    setColumns((current) => {
      if (current.collapsed === !sidebarOpen) return current;
      const next = { ...current, collapsed: !sidebarOpen };
      void homeFetch('/api/config', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ [appConfig().layoutKey]: next }),
      });
      return next;
    });
  }, [sidebarOpen, isMobile]);
  const rightMax = useCallback(() => {
    const left = document.querySelector('.tw-sidebar-host, .tw-rail')?.getBoundingClientRect().width ?? 0;
    return window.innerWidth - left - CENTER_MIN;
  }, []);
  const [navigatorOpen, setNavigatorOpen] = useState(false);
  const [view, setView] = useState<TCentreView | null>(null);
  const [designLocation, setDesignLocation] = useState<IDesignLocation>(DESIGN_HOME);
  const statsOpen = view === CENTRE_VIEW.stats;
  const efficiencyOpen = view === CENTRE_VIEW.efficiency;
  const efficiencyAlert = useMineAlert();
  const designOpen = view === CENTRE_VIEW.design;
  const newChatCounter = useRef(0);

  const themeInitialized = useRef(false);
  useEffect(() => {
    if (themeInitialized.current) return;
    themeInitialized.current = true;
    setTheme(typeof initialConfig.appTheme === 'string' ? initialConfig.appTheme : 'system');
    if (initialConfig.terminalTheme) initTerminalTheme(initialConfig.terminalTheme);
  }, [initialConfig, setTheme]);

  // A phone starts with the drawer shut, and a window that narrows to one shuts it.
  const [wasMobile, setWasMobile] = useState(false);
  if (isMobile !== wasMobile) {
    setWasMobile(isMobile);
    if (isMobile) setSidebarOpen(false);
  }

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- storage and the window size exist only after hydration
    setInspectorOpen(readInspectorOpen() && window.innerWidth >= 1100);
  }, []);

  const inspectorRestored = useRef(false);
  useEffect(() => {
    if (!inspectorRestored.current) {
      inspectorRestored.current = true;
      return;
    }
    try {
      localStorage.setItem(INSPECTOR_KEY, JSON.stringify({ open: inspectorOpen }));
    } catch {}
  }, [inspectorOpen]);

  // The panel stands in every conversation the way the person left it there: shown or hidden, its width, full screen.
  // A conversation it was never touched in starts with it hidden; what the agent makes still opens it (usePaneAutoOpen).
  const paneLayoutKey = selected && selected.key !== 'shared' ? selected.key : null;
  const [paneLayoutOf, setPaneLayoutOf] = useState<string | null>(null);
  if (paneLayoutKey !== paneLayoutOf) {
    setPaneLayoutOf(paneLayoutKey);
    // A narrow window shows the panel as a drawer over the conversation: that one opens by hand only.
    if (paneLayoutKey && !inspectorOverlay) {
      const saved = readPaneLayout(browserLayoutStorage(), paneLayoutKey);
      setInspectorOpen(saved?.open ?? false);
      setPaneMaximised(saved?.maximised ?? false);
      if (saved)
        setColumns((current) =>
          current.right === saved.width ? current : { ...current, right: saved.width },
        );
    }
  }
  useEffect(() => {
    if (!paneLayoutKey || paneLayoutKey !== paneLayoutOf || inspectorOverlay) return;
    savePaneLayout(browserLayoutStorage(), paneLayoutKey, {
      open: inspectorOpen,
      width: columns.right,
      maximised: paneMaximised,
    });
  }, [paneLayoutKey, paneLayoutOf, inspectorOverlay, inspectorOpen, columns.right, paneMaximised]);

  /** `fromAddress`: the address or a link asked for it, no click waits for its frame. */
  const select = useCallback(
    (item: Conversation, fromAddress = false) => {
      trace('nav', 'select', { key: item.key, kind: item.kind, machine: item.machineId ?? null });
      const targetMachine = item.machineId || defaultMachineId;
      const remote = Boolean(targetMachine && defaultMachineId && targetMachine !== defaultMachineId);
      // A machine on `no-frame` gets its conversation drawn by the shell, like the home machine's.
      const drawn = remote && !!targetMachine && noFrameMachines.has(targetMachine);
      markConversationOpen({
        key: item.key,
        kind: item.kind,
        provider: item.provider,
        machineId: targetMachine ?? null,
        remote,
        framed: remote && !drawn,
      });
      item = withConversationMachine(item, defaultMachineId);
      setSelected(item);
      setPending(null);
      setNavigatorOpen(false);
      setError('');
      if (item.kind === 'agent' && item.workspaceId) {
        if (remote && !drawn) {
          setViewMachine(undefined);
          setRemoteAgents((current) => visitMachineFrame(current, targetMachine!, item));
        } else {
          // Before the tab is opened: its workspace list, layout and statuses are asked of this machine.
          setViewMachine(drawn ? targetMachine : undefined);
          setAgentMounted(true);
          // The click paints its row and header first; the pane turns to the tab in the next task, so the
          // conversation's own render does not hold the frame that answers the click.
          const { workspaceId, id } = item;
          const turn = () =>
            void openAgentTab(workspaceId, id).then((opened) => {
              if (opened) markOpenPhase('tab');
            });
          if (document.hidden || fromAddress) turn();
          else requestAnimationFrame(() => setTimeout(turn, 0));
        }
      } else {
        setChatInstance((current) =>
          current?.chatId === item.id ? current : { key: item.id, chatId: item.id },
        );
      }
      if (window.innerWidth < 768) setSidebarOpen(false);
    },
    [setError, defaultMachineId, noFrameMachines],
  );

  // A search result names a moment, not just a conversation; once its timeline snapshot
  // is mounted (agent tab opened, or the chat's own timeline loaded), scroll to and flash it.
  const [pendingJumpAt, setPendingJumpAt] = useState<number | null>(null);
  const jumpSnapshotKey =
    selected?.kind === 'chat'
      ? chatInstance?.chatId
        ? `chat:${chatInstance.chatId}`
        : null
      : (activeTab?.sessionName ?? null);
  const openSearchHit = useCallback(
    (item: Conversation, atIso: string | null) => {
      select(item);
      const at = atIso ? Date.parse(atIso) : NaN;
      setPendingJumpAt(Number.isNaN(at) ? null : at);
    },
    [select],
  );
  useEffect(() => {
    if (pendingJumpAt === null || !jumpSnapshotKey) return;
    const tryJump = () => {
      const entries = useTimelineSnapshotStore.getState().bySession[jumpSnapshotKey]?.entries;
      if (!entries?.length) return false;
      const target = entries.find((entry) => entry.timestamp >= pendingJumpAt) ?? entries[entries.length - 1];
      return jumpToEntry(target.id);
    };
    // Out of the effect and the store publish: a jump may open a folded turn, which sets the timeline's state.
    let frame = 0;
    const attempt = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (tryJump()) {
          unsubscribe();
          setPendingJumpAt(null);
        }
      });
    };
    const unsubscribe = useTimelineSnapshotStore.subscribe(attempt);
    attempt();
    const timeout = window.setTimeout(() => {
      unsubscribe();
      setPendingJumpAt(null);
    }, 8_000);
    return () => {
      unsubscribe();
      cancelAnimationFrame(frame);
      window.clearTimeout(timeout);
    };
  }, [pendingJumpAt, jumpSnapshotKey]);

  // A notification can ask for a conversation before the list has loaded; it waits here until it does.
  const openTab = useCallback((tabId: string) => {
    const ref = agentRefOfTab(tabId);
    if (ref) setPending({ ref, until: Date.now() + OPEN_WAIT_MS });
  }, []);
  useEffect(() => {
    const open = (event: Event) => openTab((event as CustomEvent<string>).detail);
    window.addEventListener(OPEN_CONVERSATION_EVENT, open);
    return () => window.removeEventListener(OPEN_CONVERSATION_EVENT, open);
  }, [openTab]);
  // A board sent to the phone names the conversation to open and the page to show in its panel.
  const [pendingBoard, setPendingBoardRaw] = useState<IBoardOpenRequest | null>(null);
  // A link can come from anywhere: it opens only a page of this machine's design library, never another local port.
  const setPendingBoard = (request: IBoardOpenRequest) => {
    void ensureLibraryPort().then((port) => {
      if (port !== null && boardAddressPort(request.address) === port) setPendingBoardRaw(request);
    });
  };
  useEffect(() => {
    const open = (event: Event) => {
      const request = (event as CustomEvent<IBoardOpenRequest>).detail;
      openTab(request.tabId);
      setPendingBoard(request);
    };
    window.addEventListener(OPEN_BOARD_EVENT, open);
    return () => window.removeEventListener(OPEN_BOARD_EVENT, open);
  }, [openTab]);
  // The conversation of a board link is opened from the address (`open`); the address is rewritten to `c` once it is taken.
  const takeBoardRequest = useEffectEvent(() => {
    const board = parseBoardOpenQuery(router.query);
    if (board) setPendingBoard(board);
  });
  useEffect(() => {
    if (router.isReady) takeBoardRequest();
  }, [router.isReady]);
  // A link from Telegram (/prompt) names a tab the runtime started a second ago, and the list may not
  // have it yet. It keeps asking for a short while instead of dropping the request on the first list.
  const { refreshAgents } = data;
  const [openRetry, setOpenRetry] = useState(0);
  useEffect(() => {
    if (!pending) return;
    const item = data.agentsLoaded
      ? findConversationByRef(conversations, pending.ref, defaultMachineId)
      : undefined;
    if (item) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- opens what a link asked for once the list has it
      select(item, true);
      return;
    }
    if (Date.now() >= pending.until) {
      toast.error(t('pages.workspace.conversationNotFound'));
      setPending(null);
      return;
    }
    const timer = window.setTimeout(() => {
      if (data.agentsLoaded) void refreshAgents();
      setOpenRetry((n) => n + 1);
    }, 1500);
    return () => window.clearTimeout(timer);
  }, [pending, conversations, data.agentsLoaded, defaultMachineId, select, refreshAgents, openRetry]);

  const openView = useCallback((next: TCentreView | null) => {
    setView(next);
    if (next) {
      setSelected(null);
      setPending(null);
      if (window.innerWidth < 768) setSidebarOpen(false);
    }
  }, []);
  const openStats = useCallback((open: boolean) => openView(open ? CENTRE_VIEW.stats : null), [openView]);
  const openEfficiency = useCallback(() => openView(CENTRE_VIEW.efficiency), [openView]);
  const openDesign = useCallback(() => {
    setDesignLocation(DESIGN_HOME);
    openView(CENTRE_VIEW.design);
  }, [openView]);
  // A place of the library asked for from outside the centre: the right panel's board, a push shown in the page.
  useEffect(() => {
    const open = (event: Event) => {
      setDesignLocation((event as CustomEvent<IDesignLocation>).detail);
      openView(CENTRE_VIEW.design);
    };
    window.addEventListener(OPEN_DESIGN_EVENT, open);
    return () => window.removeEventListener(OPEN_DESIGN_EVENT, open);
  }, [openView]);

  // Any conversation opened from anywhere takes the centre pane back.
  if (selected && view) setView(null);

  const goHome = useCallback(() => {
    setView(null);
    setSelected(null);
    setPending(null);
    if (window.innerWidth < 768) setSidebarOpen(false);
  }, []);

  useEffect(() => {
    const chat = router.query.chat;
    if (!router.isReady || typeof chat !== 'string' || !/^\/s\/[a-zA-Z0-9-]+$/.test(chat)) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the router knows its query only once it is ready
    setSharedPath(`${chat}?hl-embed=chat`);
    setSelected({
      key: 'shared',
      id: 'shared',
      title: t('pages.workspace.sharedConversation'),
      updatedAt: 0,
      provider: 'chat',
      kind: 'chat',
    });
  }, [router.isReady, router.query.chat]);

  const applyLocation = useCallback(
    (location: IAppLocation) => {
      const { ref } = location;
      if (!ref) {
        setPending(null);
        setSelected(null);
        setView(location.view);
        setDesignLocation(location.design ?? DESIGN_HOME);
        return;
      }
      let item: Conversation | undefined;
      if (ref.kind === 'chat') item = findConversationByRef(conversations, ref) ?? placeholderChat(ref.id);
      else if (data.agentsLoaded) item = findConversationByRef(conversations, ref, defaultMachineId);
      if (item) {
        select(item, true);
        return;
      }
      setSelected(null);
      setView(null);
      setPending({ ref, until: Date.now() + OPEN_WAIT_MS });
    },
    [conversations, data.agentsLoaded, defaultMachineId, select],
  );
  // A conversation still being created has no address yet: the page stays where it was until the tab exists.
  const wanted = useMemo(
    () =>
      wantedLocation(
        selected?.id === UNASSIGNED_TAB ? null : selected,
        pending?.ref ?? null,
        view,
        designLocation,
      ),
    [selected, pending, view, designLocation],
  );
  const addressNav = useConversationUrl({ wanted, onLocation: applyLocation });

  /**
   * Resolves to the new agent conversation, or null: a chat is created by its first message in the chat view,
   * and a failed start shows its error in the page.
   */
  const start = useCallback(
    async (
      kind: TNewConversationKind,
      prompt = '',
      options: IStartOptions = {},
    ): Promise<Conversation | null> => {
      setError('');
      if (kind === 'chat') {
        if (!chatReady) {
          setError(
            data.chatsAbsent ? t('pages.workspace.chatsAbsent') : t('pages.workspace.chatsConnecting'),
          );
          return null;
        }
        newChatCounter.current += 1;
        setSelected({
          key: 'chat:new',
          id: 'new',
          title: prompt || t('pages.workspace.newChat'),
          provider: 'chat',
          kind: 'chat',
          updatedAt: Date.now(),
        });
        setPending(null);
        setChatInstance({
          key: `new-${newChatCounter.current}`,
          chatId: null,
          initialPrompt: prompt || undefined,
          pendingProjectId: options.projectId,
        });
        if (window.innerWidth < 768) setSidebarOpen(false);
        return null;
      }
      setBusy(true);
      try {
        // A first line that is a slash command waits for the composer instead of riding on the launch line.
        const launchPrompt = options.firstLine ? undefined : prompt;
        const startedAt = Date.now();
        setPendingLaunch(machineId, UNASSIGNED_TAB, launchPrompt ?? '', startedAt);
        let conversation: Conversation;
        // The conversation is on screen from the press: its header, right panel and first message stand where
        // they will stay while the machine creates the tab, instead of the welcome page turning into it in steps.
        // Another machine's conversation lives in its own frame, which appears with the tab: its launch stays on the welcome page.
        if (!machineId || machineId === defaultMachineId) {
          setSelected({
            key: `agent:launching:${startedAt}`,
            id: UNASSIGNED_TAB,
            title: prompt.trim() || t('pages.workspace.newConversation'),
            provider: kind,
            kind: 'agent',
            machineId: machineId || undefined,
            updatedAt: startedAt,
          });
          setPending(null);
        }
        try {
          conversation = await data.createAgent(kind, options.workspaceId, launchPrompt, machineId, {
            launch: options.launch,
          });
        } finally {
          clearPendingLaunch(machineId, UNASSIGNED_TAB);
        }
        if (!launchPrompt) markAgentLaunching(conversation.id);
        setPendingLaunch(conversation.machineId, conversation.id, launchPrompt ?? '', startedAt);
        if (options.firstLine)
          setPendingFirstCommand(conversation.machineId, conversation.id, options.firstLine);
        select(conversation);
        if (options.projectId && isConversationRef(conversation.key)) {
          const assigned = await organizerActions.assign([conversation.key], options.projectId);
          if (!assigned) toast.error(t('pages.workspace.conversationNotAssigned'));
        }
        return conversation;
      } catch (e) {
        setError(e instanceof Error ? e.message : t('pages.workspace.agentOpenFailed'));
        // The welcome page comes back with the message in its field, so nothing typed is lost.
        setSelected((current) => (current?.id === UNASSIGNED_TAB ? null : current));
        setComposeRequest((current) => ({ kind, seq: (current?.seq ?? 0) + 1, text: prompt }));
        return null;
      } finally {
        setBusy(false);
      }
    },
    [chatReady, data, machineId, defaultMachineId, select, setError],
  );

  /**
   * "Continue on machine". The agent of the conversation writes a handoff note on its own machine, and a new
   * conversation starts on the chosen one from that note alone: the history stays where it is.
   */
  const continueOn = useCallback(
    async (source: Conversation, target: ISidebarMachine) => {
      const sourceMachineName =
        fleetMachines?.find((machine) => machine.id === source.machineId)?.name ??
        source.machineName ??
        source.machineId ??
        '';
      const progress = toast.loading(t('pages.workspace.handoffWriting', { machine: target.name }), {
        description: t('pages.workspace.handoffWritingHint'),
      });
      try {
        const answer = await requestHandoffNote(source.id, source.machineId, new AbortController().signal);
        if (!answer.ok) {
          toast.error(t('pages.workspace.handoffFailed'), {
            id: progress,
            description: answer.message,
          });
          return;
        }
        const brief = buildHandoffBrief({
          title: source.title,
          machineName: sourceMachineName,
          url: conversationUrl(window.location.origin, source.key),
          note: answer.note,
        });
        const workspaceId = source.workspaceName
          ? data.workspaces.find(
              (workspace) => workspace.machineId === target.id && workspace.name === source.workspaceName,
            )?.id
          : undefined;
        const startedAt = Date.now();
        const conversation = await data.createAgent('claude', workspaceId, brief, target.id);
        setPendingLaunch(conversation.machineId, conversation.id, brief, startedAt);
        select(conversation);
        toast.success(t('pages.workspace.handoffContinues', { machine: target.name }), {
          id: progress,
          description: t('pages.workspace.handoffFirstMessage'),
        });
        const projectId = sidebar.organizer?.assignments[source.key];
        if (projectId && isConversationRef(conversation.key)) {
          const assigned = await organizerActions.assign([conversation.key], projectId);
          if (!assigned) toast.error(t('pages.workspace.conversationNotAssigned'));
        }
      } catch (e) {
        // A start that timed out may still have made the conversation: the list says whether it did.
        void data.refresh();
        toast.error(t('pages.workspace.handoffFailed'), {
          id: progress,
          description: e instanceof Error ? e.message : undefined,
        });
      }
    },
    [data, fleetMachines, select, sidebar.organizer],
  );

  const startContinueOn = useCallback(
    (source: Conversation, target: ISidebarMachine) => void continueOn(source, target),
    [continueOn],
  );

  const changeTheme = useCallback(
    (next: TThemeMode) => {
      setTheme(next);
      void homeFetch('/api/config', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ appTheme: next }),
      });
    },
    [setTheme],
  );
  const toggleTheme = useCallback(() => changeTheme(THEME_CYCLE[themeMode]), [changeTheme, themeMode]);

  /** Opens a numbered or pinned row; rows of an away machine or a deleted chat explain themselves instead. */
  const openRow = useCallback(
    (row: IRowModel) => {
      if (row.state === ROW_STATE.machineUnavailable) {
        toast.error(t('pages.workspace.machineUnavailable', { machine: row.machineName ?? '' }));
        return;
      }
      if (row.state === ROW_STATE.machineRemoved) {
        toast.error(t('pages.workspace.machineRemoved', { machine: row.machineName ?? '' }));
        return;
      }
      if (row.state === ROW_STATE.chatMissing) {
        toast.error(t('pages.workspace.chatDeleted'));
        return;
      }
      if (row.conversation) {
        select(row.conversation);
        return;
      }
      toast(t('pages.workspace.conversationLoading'));
    },
    [select],
  );

  /** Shell shortcuts from this page and from runtime iframes; true when handled, otherwise the browser keeps the key. */
  const handleShellShortcut = useCallback(
    (shortcut: IShellShortcut): boolean => {
      if (shortcut.key === 'o') {
        if (!shortcut.shift) return false;
        goHome();
        return true;
      }
      if (shortcut.shift) return false;
      if (shortcut.key === 'k') {
        setPaletteMode('default');
        setPaletteOpen((v) => !v);
      } else if (shortcut.key === 'b') setSidebarOpen((v) => !v);
      else {
        const row = model.hotkeys[Number(shortcut.key) - 1];
        if (!row) return false;
        openRow(row);
      }
      return true;
    },
    [goHome, openRow, model.hotkeys],
  );

  const hotkeyCount = model.hotkeys.length;
  const frameShortcuts = useMemo<readonly TShellShortcutId[]>(
    () => [...BASE_SHELL_SHORTCUTS, 'shift+o', ...HOTKEY_IDS.slice(0, hotkeyCount)],
    [hotkeyCount],
  );

  const { replaceNext } = addressNav;
  const travelTo = useCallback(
    (item: Conversation) => {
      replaceNext();
      select(item);
    },
    [replaceNext, select],
  );
  const history = useSelectionHistory(selected, travelTo);
  const revealProject = useSidebarView((s) => s.revealProject);
  const createFromSidebar = useCallback(
    (kind: TNewConversationKind, projectId?: string) => void start(kind, '', { projectId }),
    [start],
  );

  const reveal = useSidebarView((s) => s.reveal);
  const [shownReveal, setShownReveal] = useState<typeof reveal>(null);
  if (reveal !== shownReveal) {
    setShownReveal(reveal);
    if (reveal) setSidebarOpen(true);
  }

  useEffect(() => {
    const handle = (event: KeyboardEvent) => {
      const shortcut = shellShortcutFromEvent(event);
      if (shortcut && handleShellShortcut(shortcut)) {
        event.preventDefault();
        event.stopPropagation();
        return;
      }
      if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey) return;
      const key = event.key.toLowerCase();
      if (key === '.') setInspectorOpen((v) => !v);
      else if (key === 'j') setNavigatorOpen((v) => !v);
      else return;
      event.preventDefault();
      event.stopPropagation();
    };
    window.addEventListener('keydown', handle, true);
    return () => window.removeEventListener('keydown', handle, true);
  }, [handleShellShortcut]);

  const pendingChatProject = chatInstance?.chatId === null ? chatInstance.pendingProjectId : undefined;
  const onChatCreated = useCallback(
    (chat: { id: string; title: string }) => {
      upsertChat(chat);
      setChatInstance((current) =>
        current ? { ...current, chatId: chat.id, pendingProjectId: undefined } : current,
      );
      const item: Conversation = {
        key: chatConversationKey(chat.id),
        id: chat.id,
        title: chat.title,
        updatedAt: Date.now(),
        provider: 'chat',
        kind: 'chat',
      };
      setSelected(item);
      if (pendingChatProject && isConversationRef(item.key)) {
        void organizerActions.assign([item.key], pendingChatProject).then((ok) => {
          if (!ok) toast.error(t('pages.workspace.chatNotAssigned'));
        });
      }
    },
    [upsertChat, pendingChatProject],
  );

  const onChatTitle = useCallback(
    (id: string, title: string) => {
      upsertChat({ id, title });
      setSelected((current) => (current?.id === id ? { ...current, title } : current));
    },
    [upsertChat],
  );

  // Stable elements: sidebar polling and header state must not re-render the agent and chat trees.
  // Bumped when the default machine comes back, so the agent view starts over instead of waiting on what failed.
  const [agentEpoch, setAgentEpoch] = useState(0);
  const agentView = useMemo(() => <TerminalPage key={agentEpoch} />, [agentEpoch]);
  const chatView = useMemo(
    () =>
      chatInstance && (
        <ChatView
          key={chatInstance.key}
          chatId={chatInstance.chatId}
          initialPrompt={chatInstance.initialPrompt}
          onChatCreated={onChatCreated}
          onTitleChange={onChatTitle}
        />
      ),
    [chatInstance, onChatCreated, onChatTitle],
  );

  // A page opened on an agent conversation starts its pane and the workspace list with its first requests:
  // waiting for the conversation list first put two round trips and the pane's code in front of every first open.
  useEffect(() => {
    if (parseAppLocation(window.location.search).ref?.kind !== 'agent') return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- the address is read only after hydration
    setAgentMounted(true);
    void loadWorkspaces();
  }, []);

  useEffect(() => {
    if (agentMounted) return;
    const warm = () => setAgentMounted(true);
    const idle = window.requestIdleCallback?.(warm, { timeout: 2500 }) ?? window.setTimeout(warm, 1200);
    return () => {
      if (window.cancelIdleCallback) window.cancelIdleCallback(idle);
      else window.clearTimeout(idle);
    };
  }, [agentMounted]);

  const firstName = appConfig().people[owner.split('@')[0] ?? ''] ?? '';
  const showAgent = selected?.kind === 'agent';
  const showRemoteAgent =
    showAgent &&
    !!selected.machineId &&
    !!defaultMachineId &&
    selected.machineId !== defaultMachineId &&
    !noFrameMachines.has(selected.machineId);
  // The view machine follows what is on screen: home, a chat, a framed machine or nothing selected all ask the home machine.
  const shellDraws =
    showAgent && !showRemoteAgent && !!selected.machineId && selected.machineId !== defaultMachineId
      ? selected.machineId
      : undefined;
  useEffect(() => setViewMachine(shellDraws), [shellDraws]);
  // A conversation whose machine is down is drawn from the cluster instead of the machine's own view.
  const selectedMachineId = showAgent ? selected.machineId || defaultMachineId || null : null;
  const listedAvailable = fleetMachines?.find((m) => m.id === selectedMachineId)?.available;
  const machineReach = useMachineReach(selectedMachineId, listedAvailable);
  const machineDown = showAgent && machineReach.down;
  useEffect(() => {
    useNetworkHealthStore.getState().setMachineDown(machineDown);
    return () => useNetworkHealthStore.getState().setMachineDown(false);
  }, [machineDown]);
  // A view that came up keeps its place through an outage of its machine: the conversation, the sidebar and the
  // composer with its draft stay on screen, the timeline and status come from the cluster, and what is written waits for
  // the machine. Only a view that never came up — the page opened while the machine was already down — is stood in for
  // by the stored conversation, and only that view is started once the machine answers.
  const [machinesUp, setMachinesUp] = useState<Record<string, true>>({});
  if (machineReach.up && selectedMachineId && !machinesUp[selectedMachineId])
    setMachinesUp((current) => ({ ...current, [selectedMachineId]: true }));
  const [framesReady, setFramesReady] = useState<Record<string, true>>({});
  const frameReady = useCallback(
    (id: string) => setFramesReady((current) => (current[id] ? current : { ...current, [id]: true })),
    [],
  );
  const viewCameUp =
    !!selectedMachineId &&
    (showRemoteAgent ? !!framesReady[selectedMachineId] : agentMounted && !!machinesUp[selectedMachineId]);
  const offlineStandIn = machineDown && !viewCameUp;
  const [stoodIn, setStoodIn] = useState<Record<string, true>>({});
  if (offlineStandIn && selectedMachineId && !stoodIn[selectedMachineId])
    setStoodIn((current) => ({ ...current, [selectedMachineId]: true }));
  if (!machineDown && machineReach.up && selectedMachineId && stoodIn[selectedMachineId]) {
    const back = selectedMachineId;
    setStoodIn(
      (current) =>
        Object.fromEntries(Object.entries(current).filter(([id]) => id !== back)) as Record<string, true>,
    );
    if (back === defaultMachineId || back === viewMachine) {
      if (!agentMounted) setAgentEpoch((value) => value + 1);
    } else if (!framesReady[back])
      setFrameEpochs((current) => ({ ...current, [back]: (current[back] ?? 0) + 1 }));
  }
  // A tab the layout has not loaded yet (a conversation just created, or one from another workspace) leaves the
  // pane on the tab it showed before: an old conversation or its session list flashed in between. Until the pane
  // turns to the selected tab, its place shows the launch or a skeleton.
  const agentTabPending = showAgent && !showRemoteAgent && activeTab?.id !== selected.id;
  const pendingAgentLaunch = useMemo(
    () => (agentTabPending && selected ? readPendingLaunch(selected.machineId, selected.id) : null),
    [agentTabPending, selected],
  );
  // A tab the loaded layout already has turns within a frame: the conversation on screen dims until then instead of a
  // skeleton blinking in between. Only a tab still to load gets the placeholder.
  const agentTabLoaded = useLayoutStore(
    (s) =>
      !!selected &&
      !!s.layout &&
      collectPanes(s.layout.root).some((pane) => pane.tabs.some((tab) => tab.id === selected.id)),
  );
  // What the pane last showed on screen: dimming makes sense only over a conversation the person was looking at.
  const shownPaneTab = showAgent && !showRemoteAgent && !agentTabPending ? selected.id : null;
  const [paneTab, setPaneTab] = useState<string | null>(null);
  if (shownPaneTab !== null && shownPaneTab !== paneTab) setPaneTab(shownPaneTab);
  if ((!showAgent || showRemoteAgent) && paneTab !== null) setPaneTab(null);
  const launchProvider = selected?.provider === 'codex' ? 'codex' : 'claude';
  // A conversation the shell draws for another machine waits for that machine's first statuses too: without them
  // the pane would show its session list for a moment.
  const viewTabUnknown = useTabStore(
    (s) => !!viewMachine && selected?.kind === 'agent' && !s.tabs[selected.id],
  );
  const agentPlaceholder =
    !offlineStandIn &&
    ((agentTabPending &&
      (!agentTabLoaded || !activeTab || !!pendingAgentLaunch || activeTab.id !== paneTab)) ||
      (showAgent && !showRemoteAgent && viewTabUnknown && !machineDown));
  if (agentPlaceholder) noteOpenScreen('pane-pending');
  const showChat = selected?.kind === 'chat';
  const visibleMachineId = showRemoteAgent ? (selected?.machineId ?? null) : null;
  const [framesFor, setFramesFor] = useState(visibleMachineId);
  if (framesFor !== visibleMachineId) {
    setFramesFor(visibleMachineId);
    setRemoteAgents((current) => retainMachineFrames(current, visibleMachineId));
  }
  const liveSelected = useMemo(() => {
    if (!selected) return null;
    const fresh = conversations.find((c) => c.key === selected.key);
    return fresh ? { ...selected, title: fresh.title } : selected;
  }, [conversations, selected]);
  const canInspect = (showAgent && !offlineStandIn) || (showChat && selected?.key !== 'shared');
  const sessionName = showAgent && !showRemoteAgent ? (activeTab?.sessionName ?? null) : null;
  const slot = useMemo<IHeaderSlotProps>(
    () => ({
      conversation: liveSelected,
      machineId: liveSelected?.machineId ?? machineId,
      remote: showRemoteAgent,
      sessionName,
    }),
    [liveSelected, machineId, showRemoteAgent, sessionName],
  );

  // A machine frame draws the right panel itself; on a wide screen it spills out of the conversation island
  // into a column of its own, as the shell's panel does, instead of sitting under the header.
  const remoteSplit = showRemoteAgent && inspectorOpen && !inspectorOverlay;
  const [surface, setSurface] = useState<HTMLDivElement | null>(null);
  const paneHost = useRef<HTMLDivElement>(null);
  const [contentTop, setContentTop] = useState(0);
  useEffect(() => {
    if (!remoteSplit || !surface) return;
    const content = surface.querySelector<HTMLElement>('.tw-content');
    if (!content) return;
    const measure = () =>
      setContentTop(content.getBoundingClientRect().top - surface.getBoundingClientRect().top);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(surface);
    return () => observer.disconnect();
  }, [remoteSplit, surface]);
  const rightWidth = columns.right;
  const frameSplit = useMemo(
    () => (remoteSplit && contentTop > 0 ? { top: contentTop, right: rightWidth } : null),
    [remoteSplit, contentTop, rightWidth],
  );

  useBackLayer(isMobile && sidebarOpen, () => setSidebarOpen(false));
  useBackLayer(inspectorOverlay && canInspect && inspectorOpen, () => setInspectorOpen(false));
  // Phone: from the left edge towards the middle opens the conversations, the other way the right panel; each swipe back closes it.
  const swipe = useCallback(
    (direction: TSwipeDirection) => {
      if (direction === 'right') {
        if (inspectorOpen && canInspect) setInspectorOpen(false);
        else setSidebarOpen(true);
      } else if (sidebarOpen) setSidebarOpen(false);
      else if (canInspect) setInspectorOpen(true);
    },
    [inspectorOpen, sidebarOpen, canInspect],
  );
  useTouchGestures(isMobile, swipe);
  usePinnedViewport(isMobile);
  useViewportReport(isMobile);

  // `openPaneTab` shows the panel with that tab; a machine's conversation gets it in its frame (MachineRuntimeFrame).
  const paneMode = showChat ? 'chat' : 'agent';
  const paneKey = paneKeyOf(
    paneMode,
    showAgent ? (activeTab?.id ?? null) : null,
    showChat ? (chatInstance?.chatId ?? null) : null,
  );
  useEffect(() => {
    const open = (event: Event) => {
      const request = (event as CustomEvent<unknown>).detail;
      if (!isPaneTabRequest(request) || !canInspect) return;
      // A plain chat has no machine behind it: a tool that needs one is not a tab here, and a link to it stays a link.
      if (paneMode === 'chat' && PANE_KINDS[request.kind].agentOnly) return;
      event.preventDefault();
      setInspectorOpen(true);
      if (!showRemoteAgent && paneKey) applyPaneRequest(paneKey, request);
    };
    window.addEventListener(OPEN_PANE_TAB_EVENT, open);
    return () => window.removeEventListener(OPEN_PANE_TAB_EVENT, open);
  }, [canInspect, showRemoteAgent, paneKey, paneMode]);

  useEffect(() => {
    if (!pendingBoard || selected?.kind !== 'agent' || selected.id !== pendingBoard.tabId || !canInspect)
      return;
    const open = () => openPaneTab('browser', pendingBoard.address, pendingBoard.title || null);
    if (!showRemoteAgent) {
      if (paneKey !== pendingBoard.tabId) return;
      open();
      // eslint-disable-next-line react-hooks/set-state-in-effect -- the board opens once its conversation is on screen
      setPendingBoardRaw(null);
      return;
    }
    open();
    const timers = REMOTE_BOARD_RETRY_MS.map((ms) => window.setTimeout(open, ms));
    timers.push(
      window.setTimeout(
        () => setPendingBoardRaw(null),
        REMOTE_BOARD_RETRY_MS[REMOTE_BOARD_RETRY_MS.length - 1] + 500,
      ),
    );
    return () => timers.forEach((timer) => window.clearTimeout(timer));
  }, [pendingBoard, selected, canInspect, showRemoteAgent, paneKey]);

  usePaneAutoOpen({
    mode: paneMode,
    tab: showAgent && !showRemoteAgent ? activeTab : null,
    chatId: showChat ? (chatInstance?.chatId ?? null) : null,
    active: canInspect && !showRemoteAgent,
    paneVisible: inspectorOpen,
    overlay: isMobile,
    onShow: () => setInspectorOpen(true),
  });

  // What the agent of this machine's conversation asked its pane for, and its live `panel hide|status`; a
  // conversation of another machine gets both in its frame (RuntimeWorkspace).
  const localAgentTabId = showAgent && !showRemoteAgent ? (activeTab?.id ?? null) : null;
  usePaneIntents({
    tabId: localAgentTabId,
    active: canInspect && !showRemoteAgent,
    paneVisible: inspectorOpen,
    overlay: isMobile,
    onShow: () => setInspectorOpen(true),
  });
  useAgentPanelCommands(localAgentTabId);
  useEffect(() => {
    if (!localAgentTabId) return;
    const answer = (event: Event, value: unknown) => {
      event.preventDefault();
      (event as CustomEvent<{ respond?: (value: unknown) => void } | null>).detail?.respond?.(value);
    };
    const setVisible = (event: Event) => {
      const visible = (event as CustomEvent<{ visible?: unknown } | null>).detail?.visible;
      if (typeof visible !== 'boolean') return;
      setInspectorOpen(visible);
      answer(event, {
        ok: true,
        message: visible ? t('pages.workspace.panelShown') : t('pages.workspace.panelHidden'),
      });
    };
    const state = (event: Event) => {
      const pane = readPane(localAgentTabId);
      answer(event, {
        visible: inspectorOpen,
        open: pane.tabs.map((paneTab) => paneTab.kind),
        active: pane.tabs.find((paneTab) => paneTab.id === pane.activeId)?.kind ?? null,
      });
    };
    window.addEventListener(SET_PANE_VISIBLE_EVENT, setVisible);
    window.addEventListener(GET_PANE_STATE_EVENT, state);
    return () => {
      window.removeEventListener(SET_PANE_VISIBLE_EVENT, setVisible);
      window.removeEventListener(GET_PANE_STATE_EVENT, state);
    };
  }, [localAgentTabId, inspectorOpen]);

  useEffect(() => {
    if (!canInspect) return;
    const handle = (event: KeyboardEvent) => {
      const kind = paneShortcutFromEvent(event);
      if (!kind) return;
      event.preventDefault();
      event.stopPropagation();
      openPaneTab(kind);
    };
    window.addEventListener('keydown', handle, true);
    return () => window.removeEventListener('keydown', handle, true);
  }, [canInspect]);

  const paneShown = canInspect && !showRemoteAgent && inspectorOpen;
  usePaneColumnFlip(paneShown && !paneMaximised, surface, paneHost, !inspectorOverlay);
  // An overlay pane stays mounted once opened, so the next open is a slide with its tab and scroll kept,
  // not a thousand components mounting mid-animation.
  const [paneMounted, setPaneMounted] = useState(false);
  if (inspectorOverlay && paneShown && !paneMounted) setPaneMounted(true);
  // Once it has slid away, the hidden pane sleeps: its effects stop (no polling behind the screen), its state stays.
  const [paneAsleep, setPaneAsleep] = useState(false);
  if (paneShown && paneAsleep) setPaneAsleep(false);
  useEffect(() => {
    if (!paneShown || !paneMaximised) return;
    const restore = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) setPaneMaximised(false);
    };
    window.addEventListener('keydown', restore);
    return () => window.removeEventListener('keydown', restore);
  }, [paneShown, paneMaximised]);

  const toggleSummary = () => {
    if (showRemoteAgent) window.dispatchEvent(new Event(PANE_SUMMARY_MESSAGE));
    else setSummaryOpen((open) => !open);
  };

  const panePanel = (
    <ConversationPane
      mode={paneMode}
      tab={showAgent && !agentTabPending ? activeTab : null}
      launching={showAgent && agentTabPending}
      chatId={showChat ? (chatInstance?.chatId ?? null) : null}
      provider={selected?.provider ?? 'claude'}
      maximised={paneMaximised && !inspectorOverlay}
      onToggleMaximised={() => setPaneMaximised((value) => !value)}
      onHide={() => setInspectorOpen(false)}
    />
  );
  const sidebarPanel = (
    <ConversationSidebar
      owner={owner}
      conversations={conversations}
      model={model}
      selectedMachineId={machineId}
      onSelectMachine={setChosenMachineId}
      loading={data.loading}
      organizerReady={sidebar.organizer !== null}
      organizerError={sidebar.organizerError}
      organizerMachineName={organizerMachineName}
      onRetryOrganizer={() => void sidebar.refreshOrganizer()}
      hasMore={data.hasMore}
      history={history}
      themeMode={themeMode}
      onToggleTheme={toggleTheme}
      onThemeChange={changeTheme}
      onNew={goHome}
      onLoadMore={() => void data.loadMore()}
      onOpenPalette={() => {
        setPaletteMode('default');
        setPaletteOpen(true);
      }}
      onOpenSearch={openSearchPalette}
      statsOpen={statsOpen}
      onOpenStats={() => openStats(true)}
      efficiencyOpen={efficiencyOpen}
      efficiencyAlert={efficiencyAlert}
      onOpenEfficiency={appConfig().features.efficiency ? openEfficiency : undefined}
      designOpen={designOpen}
      onOpenDesign={appConfig().features.design ? openDesign : undefined}
      onCollapse={() => setSidebarOpen(false)}
    />
  );

  // A board alone in the window: none of the shell around it, so it is the whole screen on a phone as well.
  if (designOpen && !selected && designLocation.full) {
    return (
      <RuntimeMachineContext.Provider value={defaultMachineId}>
        <div className="tw-root">
          <LibraryHome location={designLocation} onNavigate={setDesignLocation} />
        </div>
      </RuntimeMachineContext.Provider>
    );
  }

  return (
    <RuntimeMachineContext.Provider value={defaultMachineId}>
      <SWRConfig
        value={{
          fallback: { '/api/quick-prompts': initialQuickPrompts, '/api/sidebar-items': initialSidebarItems },
        }}
      >
        <SidebarProvider
          state={sidebar}
          selectedKey={selected?.key ?? null}
          busy={busy}
          features={features}
          onSelect={select}
          onOpenRow={openRow}
          onCreate={createFromSidebar}
          onContinueOn={startContinueOn}
        >
          <Head>
            <title>
              {selected
                ? `${selected.title.slice(0, 60)} · ${appConfig().productName}`
                : statsOpen
                  ? `${t('pages.workspace.stats')} · ${appConfig().productName}`
                  : efficiencyOpen
                    ? `${t('pages.workspace.efficiency')} · ${appConfig().productName}`
                    : designOpen
                      ? `Claude Design · ${appConfig().productName}`
                      : appConfig().productName}
            </title>
          </Head>
          <div
            className="tw-root"
            data-sidebar={sidebarOpen ? 'open' : 'closed'}
            data-remote-split={frameSplit ? '' : undefined}
            style={
              {
                '--tw-left-w': `${columns.left}px`,
                '--tw-right-w': `${rightWidth}px`,
                '--tw-content-top': `${contentTop}px`,
              } as CSSProperties
            }
          >
            {isMobile ? (
              drawerMounted && (
                <div
                  className="tw-sidebar-host tw-sidebar-drawer"
                  data-open={sidebarOpen || undefined}
                  inert={!sidebarOpen}
                  aria-hidden={!sidebarOpen}
                >
                  {sidebarPanel}
                </div>
              )
            ) : (
              <AnimatePresence initial={false}>
                {sidebarOpen && (
                  <motion.div
                    key="sidebar"
                    className="tw-sidebar-host"
                    {...COLUMN}
                    transition={{ duration: 0.28, ease: [0.32, 0.72, 0, 1] }}
                  >
                    {sidebarPanel}
                    <ColumnResizer
                      side="left"
                      width={columns.left}
                      min={220}
                      max={440}
                      defaultWidth={LEFT_DEFAULT}
                      cssVar="--tw-left-w"
                      onCommit={(w) => commitColumn('left', w)}
                    />
                  </motion.div>
                )}
              </AnimatePresence>
            )}
            {!sidebarOpen && !isMobile && (
              <ConversationRail
                owner={owner}
                model={model}
                themeMode={themeMode}
                onToggleTheme={toggleTheme}
                onOpenPalette={() => {
                  setPaletteMode('default');
                  setPaletteOpen(true);
                }}
                statsOpen={statsOpen}
                onOpenStats={() => openStats(true)}
                efficiencyOpen={efficiencyOpen}
                efficiencyAlert={efficiencyAlert}
                onOpenEfficiency={appConfig().features.efficiency ? openEfficiency : undefined}
                designOpen={designOpen}
                onOpenDesign={appConfig().features.design ? openDesign : undefined}
                onExpand={() => setSidebarOpen(true)}
              />
            )}
            {sidebarOpen && isMobile && (
              <button
                type="button"
                className="tw-scrim"
                aria-label={t('pages.workspace.closePanel')}
                onClick={() => setSidebarOpen(false)}
              />
            )}

            <main className="tw-main" data-pane={paneShown && !inspectorOverlay ? '' : undefined}>
              <div ref={setSurface} className="tw-surface">
                <WorkspaceHeader
                  selected={liveSelected}
                  opening={pending !== null && !selected}
                  heading={
                    liveSelected
                      ? undefined
                      : statsOpen
                        ? t('pages.workspace.stats')
                        : efficiencyOpen
                          ? t('pages.workspace.efficiency')
                          : designOpen
                            ? 'Claude Design'
                            : undefined
                  }
                  sidebarOpen={sidebarOpen || !isMobile}
                  onToggleSidebar={() => setSidebarOpen((v) => !v)}
                  onHome={goHome}
                  onNew={goHome}
                  slot={slot}
                  onSearch={canInspect ? () => setNavigatorOpen(true) : undefined}
                  actions={
                    canInspect && (
                      <>
                        <button
                          ref={summaryAnchor}
                          type="button"
                          className={`tw-icon-button tw-rpane-summary-toggle${summaryOpen ? ' tw-icon-button-on' : ''}`}
                          aria-label={t('pages.workspace.summaryLabel')}
                          aria-expanded={showRemoteAgent ? undefined : summaryOpen}
                          title={t('pages.workspace.summary')}
                          onClick={toggleSummary}
                        >
                          <ListTodo />
                        </button>
                        {!inspectorOpen && (
                          <button
                            type="button"
                            className="tw-icon-button tw-inspector-toggle"
                            data-testid="pane-toggle"
                            aria-label={t('pages.workspace.showRightPanel')}
                            title={`${t('pages.workspace.showRightPanel')} · ⌘.`}
                            onClick={() => setInspectorOpen(true)}
                          >
                            <PanelRight />
                          </button>
                        )}
                      </>
                    )
                  }
                />
                {data.error && !machineDown && (
                  <div className="tw-error" role="alert">
                    <span>{data.error}</span>
                    <button
                      type="button"
                      onClick={() => {
                        setError('');
                        void refresh();
                      }}
                    >
                      {t('pages.workspace.retry')}
                    </button>
                  </div>
                )}
                <AppUpdateBanner />
                {lastingFleetErrors.length > 0 && !machineDown && (
                  <div className="tw-error tw-error-floating" role="status">
                    {t('pages.workspace.someMachinesDown')}
                    <span title={lastingFleetErrors.join('; ')}> {lastingFleetErrors.length}</span>
                  </div>
                )}
                <section className="tw-content">
                  {statsOpen && !selected && <StatsView />}
                  {efficiencyOpen && !selected && <EfficiencyView />}
                  {designOpen && !selected && (
                    <LibraryHome location={designLocation} onNavigate={setDesignLocation} />
                  )}
                  {!selected && !view && pending && <ConversationSkeleton />}
                  {!selected && !view && !pending && (
                    <WorkspaceWelcome
                      name={firstName}
                      busy={busy}
                      recent={conversations.slice(0, 4)}
                      recentLoading={data.loading}
                      machineId={machineId}
                      features={features}
                      workspaces={machineWorkspaces}
                      composeRequest={composeRequest}
                      onStart={(kind, prompt, options) => void start(kind, prompt, options)}
                      onSelect={select}
                    />
                  )}
                  {agentPlaceholder && (
                    <div className="tw-pane tw-pane-placeholder">
                      {pendingAgentLaunch ? (
                        <div className="flex h-full flex-col pb-3">
                          <div className="min-h-0 flex-1">
                            <LaunchPreview launch={pendingAgentLaunch} provider={launchProvider} />
                          </div>
                          <LaunchComposer
                            tabId={selected?.id ?? UNASSIGNED_TAB}
                            machineId={selected?.machineId ?? machineId ?? undefined}
                            provider={launchProvider}
                          />
                        </div>
                      ) : (
                        <ConversationSkeleton />
                      )}
                    </div>
                  )}
                  {agentMounted && (
                    <div
                      className={`tw-pane${showAgent && !showRemoteAgent && !agentPlaceholder && !offlineStandIn ? '' : ' tw-pane-off'}${agentTabPending && !agentPlaceholder ? ' tw-pane-turning' : ''}`}
                      aria-hidden={!showAgent || showRemoteAgent || agentPlaceholder || offlineStandIn}
                    >
                      {machineDown && !offlineStandIn && !showRemoteAgent && (
                        <div className="tw-offline-banner tw-offline-banner-live" role="status">
                          <CloudOff aria-hidden />
                          <span>
                            <b>
                              {fleetMachines?.find((m) => m.id === selectedMachineId)?.name ??
                                selectedMachineId}
                            </b>{' '}
                            {machineReach.since
                              ? t('pages.workspace.machineOfflineSince', {
                                  time: new Date(machineReach.since).toLocaleTimeString(locale(), {
                                    hour: '2-digit',
                                    minute: '2-digit',
                                  }),
                                })
                              : t('pages.workspace.machineOffline')}
                          </span>
                        </div>
                      )}
                      <RuntimeMachineContext.Provider value={viewMachine ?? defaultMachineId}>
                        <TerminalSurfaceVisibleContext.Provider
                          value={showAgent && !showRemoteAgent && !agentPlaceholder && !offlineStandIn}
                        >
                          {agentView}
                        </TerminalSurfaceVisibleContext.Provider>
                      </RuntimeMachineContext.Provider>
                    </div>
                  )}
                  {offlineStandIn && liveSelected && (
                    <OfflineConversation
                      key={liveSelected.key}
                      conversation={{ ...liveSelected, machineId: selectedMachineId ?? undefined }}
                      machineName={
                        fleetMachines?.find((m) => m.id === selectedMachineId)?.name ??
                        liveSelected.machineName ??
                        selectedMachineId ??
                        t('pages.workspace.machine')
                      }
                      since={machineReach.since}
                    />
                  )}
                  {Object.entries(remoteAgents).map(([id, { conversation }]) => (
                    <MachineRuntimeFrame
                      key={`${id}:${frameEpochs[id] ?? 0}`}
                      machineId={id}
                      conversation={conversation}
                      visible={visibleMachineId === id && !(offlineStandIn && visibleMachineId === id)}
                      onReady={frameReady}
                      inspectorOpen={inspectorOpen}
                      navigatorOpen={navigatorOpen}
                      onNavigatorChange={setNavigatorOpen}
                      onInspectorChange={setInspectorOpen}
                      onPaneWidth={(width) => commitColumn('right', width)}
                      shellShortcuts={frameShortcuts}
                      onShortcut={handleShellShortcut}
                      onSwipe={swipe}
                      split={frameSplit}
                    />
                  ))}
                  {chatReady && chatView && (
                    <div
                      className={`tw-pane${showChat && selected?.key !== 'shared' ? '' : ' tw-pane-off'}`}
                      aria-hidden={!showChat || selected?.key === 'shared'}
                    >
                      {chatView}
                    </div>
                  )}
                  {sharedPath && selected?.key === 'shared' && (
                    <iframe
                      title={t('pages.workspace.sharedConversation')}
                      src={sharedPath}
                      className="tw-pane tw-frame"
                      allow="clipboard-read; clipboard-write"
                    />
                  )}
                </section>
              </div>
              {paneShown && (
                <button
                  type="button"
                  className="tw-scrim tw-inspector-scrim"
                  aria-label={t('pages.workspace.closeRightPanel')}
                  onClick={() => setInspectorOpen(false)}
                />
              )}
              {inspectorOverlay ? (
                paneMounted && (
                  <div
                    className="tw-inspector-host tw-inspector-drawer"
                    data-open={paneShown || undefined}
                    data-maximised={paneMaximised || undefined}
                    inert={!paneShown}
                    aria-hidden={!paneShown}
                    onTransitionEnd={(event) => {
                      if (event.target === event.currentTarget && !paneShown) setPaneAsleep(true);
                    }}
                  >
                    <Activity mode={paneAsleep ? 'hidden' : 'visible'}>{panePanel}</Activity>
                  </div>
                )
              ) : (
                <AnimatePresence initial={false}>
                  {paneShown && (
                    <div
                      key="inspector"
                      ref={paneHost}
                      className="tw-inspector-host"
                      data-maximised={paneMaximised || undefined}
                    >
                      {!paneMaximised && (
                        <ColumnResizer
                          side="right"
                          width={columns.right}
                          min={RIGHT_MIN}
                          max={rightMax}
                          defaultWidth={RIGHT_DEFAULT}
                          cssVar="--tw-right-w"
                          onCommit={(w) => commitColumn('right', w)}
                        />
                      )}
                      {panePanel}
                    </div>
                  )}
                </AnimatePresence>
              )}
              {canInspect && !showRemoteAgent && (
                <PaneSummary
                  open={summaryOpen}
                  onClose={() => setSummaryOpen(false)}
                  anchorRef={summaryAnchor}
                  mode={paneMode}
                  tab={showAgent ? activeTab : null}
                  chatId={showChat ? (chatInstance?.chatId ?? null) : null}
                />
              )}
            </main>
          </div>
          <AgentNotifier
            conversations={conversations}
            selected={selected}
            defaultMachineId={defaultMachineId}
            onOpen={select}
          />
          <CommandPalette
            open={paletteOpen}
            onOpenChange={setPaletteOpen}
            initialMode={paletteMode}
            onOpenSearchHit={openSearchHit}
            conversations={conversations}
            model={model}
            current={liveSelected}
            theme={theme}
            onCreate={(kind) => void start(kind)}
            onRevealProject={revealProject}
            onToggleTheme={toggleTheme}
            onToggleSidebar={() => setSidebarOpen((v) => !v)}
            onToggleInspector={() => setInspectorOpen((v) => !v)}
            onOpenStats={() => openStats(true)}
            onOpenEfficiency={appConfig().features.efficiency ? openEfficiency : undefined}
            efficiencyAlert={efficiencyAlert}
            onOpenDesign={appConfig().features.design ? openDesign : undefined}
          />
          {selected && !showRemoteAgent && (
            <ConversationNavigator
              open={navigatorOpen}
              onOpenChange={setNavigatorOpen}
              snapshotKey={jumpSnapshotKey}
            />
          )}
        </SidebarProvider>
      </SWRConfig>
    </RuntimeMachineContext.Provider>
  );
};

const Workspace: TNextPageWithLayout<IWorkspaceProps> = (props) => {
  const { query } = useRouter();
  if (query.runtime !== '1') return <WorkspaceShell {...props} />;
  // Runtime iframe of a fleet machine: the page is prerendered, so the machine it shows comes from `?machine=`.
  const machine = query.machine;
  return (
    <RuntimeWorkspace
      {...props}
      runtimeMachineId={typeof machine === 'string' && machine ? machine : null}
      initialView={parseFrameView(query)}
    />
  );
};

Workspace.bootstrap = true;

export default Workspace;
