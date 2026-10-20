import { savedLayoutOf } from '@/platform/config';
import dynamic from 'next/dynamic';
import { useCallback, useEffect, useRef, useState } from 'react';
import { SWRConfig } from 'swr';
import useTabStore from '@/hooks/use-tab-store';
import useSync from '@/hooks/use-sync';
import useActiveAgentTab from '@/hooks/use-active-agent-tab';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import { loadWorkspaces, openAgentTab } from '@/lib/hl-open-agent';
import { initTerminalTheme } from '@/hooks/use-terminal-theme';
import { NETWORK_MESSAGE_TYPE, selectNetworkQuality, useNetworkHealthStore } from '@/lib/network-health';
import {
  PANE_SUMMARY_MESSAGE,
  PANE_WIDTH_MESSAGE,
  SHORTCUT_MESSAGE,
  SWIPE_MESSAGE,
  isFrameSplit,
  isOpenPaneTabMessage,
  isShortcutIdList,
  shellShortcutFromEvent,
  shortcutId,
  type IFrameSplit,
  type IFrameView,
  type TShellShortcutId,
} from '@/lib/hl-frame-bridge';
import {
  OPEN_PANE_TAB_EVENT,
  isPaneTabRequest,
  paneShortcutFromEvent,
  type IPaneTabRequest,
} from '@/lib/hl-pane-tabs';
import { applyPaneRequest, openPaneTab, readPane } from '@/lib/hl-pane-store';
import { GET_PANE_STATE_EVENT, SET_PANE_VISIBLE_EVENT } from '@/lib/hl-pane-bus';
import type { IConfigInitialData } from '@/hooks/use-config-store';
import type { IQuickPromptsData } from '@/lib/quick-prompts-store';
import type { ISidebarItemsData } from '@/lib/sidebar-items-store';
import ColumnResizer from '@/components/agents/column-resizer';
import useIsMobile from '@/hooks/use-is-mobile';
import useAgentPanelCommands from '@/hooks/use-agent-panel-commands';
import VaultAgentRequests from '@/components/agents/vault/vault-agent-requests';
import usePaneAutoOpen from '@/hooks/use-pane-auto-open';
import usePaneIntents from '@/hooks/use-pane-intents';
import useTouchGestures from '@/hooks/use-touch-gestures';
import usePinnedViewport from '@/hooks/use-pinned-viewport';
import { TerminalSurfaceVisibleContext } from '@/hooks/use-terminal-surface-visible';
import { setFrameHidden } from '@/lib/frame-visibility';
import { t } from '@/lib/i18n';

const TerminalPage = dynamic(() => import('@/components/workspace/terminal-page'), { ssr: false });
// The island stands at its width while its code loads: an empty host let the conversation fill the column and then
// narrow, reflowing every reply.
const ConversationPane = dynamic(() => import('@/components/agents/pane/conversation-pane'), {
  ssr: false,
  loading: () => <section className="tw-rpane" aria-hidden />,
});
const PaneSummary = dynamic(() => import('@/components/agents/pane/pane-summary'), { ssr: false });
const FloatingPreview = dynamic(() => import('@/components/agents/inspector/floating-preview'), {
  ssr: false,
});
const ConversationNavigator = dynamic(() => import('@/components/agents/conversation-navigator'), {
  ssr: false,
});

const PANE_WIDTH_KEY = 'hl:pane-width';
const PANE_MIN = 360;
const PANE_DEFAULT = 480;

const paneMax = () => Math.max(PANE_MIN, window.innerWidth - 440);

const initialPaneWidth = (configured: number | undefined): number => {
  try {
    const saved = Number(localStorage.getItem(PANE_WIDTH_KEY));
    if (Number.isFinite(saved) && saved >= PANE_MIN) return saved;
  } catch {}
  return configured !== undefined && configured >= PANE_MIN ? configured : PANE_DEFAULT;
};

interface IRuntimeWorkspaceProps {
  /** Provided as RuntimeMachineContext, like the shell does for its default machine (pending commands keys, fleet status). */
  runtimeMachineId?: string | null;
  /** The shell's view when it created this frame (`?pane=&split=`), so the first paint already has the panel's column. */
  initialView?: IFrameView;
  initialConfig: IConfigInitialData;
  initialQuickPrompts: IQuickPromptsData;
  initialSidebarItems: ISidebarItemsData;
}

const RuntimeWorkspace = ({
  runtimeMachineId,
  initialView,
  initialConfig,
  initialQuickPrompts,
  initialSidebarItems,
}: IRuntimeWorkspaceProps) => {
  const savedLayout = savedLayoutOf<{ left?: number; right?: number; collapsed?: boolean }>(initialConfig);
  useSync();
  const activeTab = useActiveAgentTab();
  useAgentPanelCommands(activeTab?.id ?? null);
  const [inspectorOpen, setInspectorOpen] = useState(initialView?.inspectorOpen ?? false);
  const [navigatorOpen, setNavigatorOpen] = useState(false);
  const [provider, setProvider] = useState('claude');
  const [width, setWidth] = useState(() => initialPaneWidth(savedLayout?.right));
  const [maximised, setMaximised] = useState(false);
  const [summaryOpen, setSummaryOpen] = useState(false);
  const summaryAnchor = useRef<HTMLSpanElement>(null);
  const [error, setError] = useState('');
  const [split, setSplit] = useState<IFrameSplit | null>(initialView?.split ?? null);
  /** The shell keeps the last machine left mounted but hidden; nothing here should paint or poll for it then. */
  const [frameVisible, setFrameVisible] = useState(true);
  /** Shortcuts the shell around this frame handles; until it says, every key stays in the frame. */
  const [shellShortcuts, setShellShortcuts] = useState<readonly TShellShortcutId[]>([]);
  const isMobile = useIsMobile();
  const paneOverlay = useIsMobile(1100);
  const tabId = activeTab?.id ?? null;
  // The drawers belong to the shell around this frame: a swipe here is reported, the keyboard is put away here.
  useTouchGestures(isMobile, (direction) =>
    window.parent.postMessage({ type: SWIPE_MESSAGE, direction }, location.origin),
  );
  usePinnedViewport(isMobile);
  const changeNavigator = useCallback((open: boolean) => {
    setNavigatorOpen(open);
    window.parent.postMessage({ type: 'hl:navigator-state', open }, location.origin);
  }, []);
  const changeInspector = useCallback((open: boolean) => {
    setInspectorOpen(open);
    window.parent.postMessage({ type: 'hl:inspector-state', open }, location.origin);
  }, []);

  // In the shell's split the column is the shell's: a drag here asks the shell to keep it at this width.
  const commitWidth = useCallback((next: number) => {
    setWidth(next);
    try {
      localStorage.setItem(PANE_WIDTH_KEY, String(next));
    } catch {}
    if (window.parent !== window)
      window.parent.postMessage({ type: PANE_WIDTH_MESSAGE, width: next }, location.origin);
  }, []);

  const openTabHere = useCallback(
    (request: IPaneTabRequest) => {
      changeInspector(true);
      if (tabId) applyPaneRequest(tabId, request);
    },
    [changeInspector, tabId],
  );

  usePaneAutoOpen({
    mode: 'agent',
    tab: activeTab,
    chatId: null,
    active: frameVisible,
    paneVisible: inspectorOpen,
    overlay: isMobile,
    onShow: () => changeInspector(true),
  });

  usePaneIntents({
    tabId,
    active: frameVisible,
    paneVisible: inspectorOpen,
    overlay: isMobile,
    onShow: () => changeInspector(true),
  });

  useEffect(() => {
    if (!inspectorOpen || !maximised) return;
    const restore = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) setMaximised(false);
    };
    window.addEventListener('keydown', restore);
    return () => window.removeEventListener('keydown', restore);
  }, [inspectorOpen, maximised]);

  // An attribute rather than `html:has(...)`: a `:has()` on the root makes every DOM change restyle the whole document.
  useEffect(() => {
    const root = document.documentElement;
    if (split) root.dataset.runtimeSplit = '';
    else delete root.dataset.runtimeSplit;
    return () => {
      delete root.dataset.runtimeSplit;
    };
  }, [split]);

  useEffect(() => {
    setFrameHidden(!frameVisible);
    const root = document.documentElement;
    if (frameVisible) delete root.dataset.frameHidden;
    else root.dataset.frameHidden = '';
  }, [frameVisible]);

  useEffect(() => {
    if (initialConfig.terminalTheme) initTerminalTheme(initialConfig.terminalTheme);
  }, [initialConfig]);

  useEffect(() => {
    let selectionSequence = 0;
    const handle = (event: MessageEvent) => {
      if (event.origin !== location.origin || event.source !== window.parent) return;
      if (event.data?.type === 'hl:open-agent') {
        const { workspaceId, tabId, provider: nextProvider } = event.data;
        if (typeof workspaceId !== 'string' || typeof tabId !== 'string') return;
        if (typeof nextProvider === 'string') setProvider(nextProvider);
        setError('');
        const sequence = ++selectionSequence;
        void openAgentTab(workspaceId, tabId)
          .then((opened) => {
            if (sequence === selectionSequence && !opened) setError(t('agents.runtimeWorkspace.openFailed'));
          })
          .catch(() => {
            if (sequence === selectionSequence) setError(t('agents.runtimeWorkspace.machineUnavailable'));
          });
      }
      if (event.data?.type === 'hl:runtime-view') {
        if (typeof event.data.inspectorOpen === 'boolean') setInspectorOpen(event.data.inspectorOpen);
        if (typeof event.data.navigatorOpen === 'boolean') setNavigatorOpen(event.data.navigatorOpen);
        if (isShortcutIdList(event.data.shortcuts)) setShellShortcuts(event.data.shortcuts);
        setSplit(isFrameSplit(event.data.split) ? event.data.split : null);
        if (typeof event.data.visible === 'boolean') setFrameVisible(event.data.visible);
      }
      // The shell's tab requests and summary button open them here, where the right panel of this machine lives.
      if (isOpenPaneTabMessage(event.data)) {
        const { kind, target, title } = event.data;
        window.dispatchEvent(
          new CustomEvent<IPaneTabRequest>(OPEN_PANE_TAB_EVENT, { detail: { kind, target, title } }),
        );
      }
      if (event.data?.type === PANE_SUMMARY_MESSAGE) setSummaryOpen((open) => !open);
    };
    window.addEventListener('message', handle);
    // The workspace list is on its way while the shell answers with the conversation to open.
    void loadWorkspaces();
    window.parent.postMessage({ type: 'hl:agents-ready' }, location.origin);
    return () => {
      selectionSequence++;
      window.removeEventListener('message', handle);
    };
  }, []);

  useEffect(() => {
    const publish = () => {
      const statuses = Object.fromEntries(
        Object.entries(useTabStore.getState().tabs).map(([id, tab]) => [
          id,
          tab.cliState === 'busy'
            ? 'working'
            : tab.cliState === 'needs-input'
              ? 'needs-input'
              : tab.cliState === 'ready-for-review'
                ? 'ready'
                : 'idle',
        ]),
      );
      window.parent.postMessage({ type: 'hl:agent-status', statuses }, location.origin);
    };
    publish();
    return useTabStore.subscribe((state, previous) => {
      if (state.tabs !== previous.tabs) publish();
    });
  }, []);

  // The header with the network chip lives in the page around this frame: tell it how this machine's link feels.
  const networkQuality = useNetworkHealthStore(selectNetworkQuality);
  useEffect(() => {
    window.parent.postMessage({ type: NETWORK_MESSAGE_TYPE, quality: networkQuality }, location.origin);
  }, [networkQuality]);

  // An agent's command arrives as these events and waits for `respond`; the buttons fire them without one.
  useEffect(() => {
    const answer = (event: Event, value: unknown) => {
      event.preventDefault();
      (event as CustomEvent<{ respond?: (value: unknown) => void } | null>).detail?.respond?.(value);
    };
    const open = (event: Event) => {
      const request = (event as CustomEvent<unknown>).detail;
      if (!isPaneTabRequest(request)) return;
      if (!tabId) {
        // Not prevented: a link that asked for a tab opens as a link, an agent hears the reason.
        (event as CustomEvent<{ respond?: (value: unknown) => void } | null>).detail?.respond?.({
          ok: false,
          message: t('agents.runtimeWorkspace.noConversation'),
        });
        return;
      }
      openTabHere(request);
      answer(event, {
        ok: true,
        message: request.target
          ? t('agents.runtimeWorkspace.openedTabTarget', { kind: request.kind, target: request.target })
          : t('agents.runtimeWorkspace.openedTab', { kind: request.kind }),
      });
    };
    const setVisible = (event: Event) => {
      const visible = (event as CustomEvent<{ visible?: unknown } | null>).detail?.visible;
      if (typeof visible !== 'boolean') return;
      changeInspector(visible);
      answer(event, {
        ok: true,
        message: visible ? t('agents.runtimeWorkspace.shownPane') : t('agents.runtimeWorkspace.hiddenPane'),
      });
    };
    const state = (event: Event) => {
      const pane = tabId ? readPane(tabId) : null;
      answer(event, {
        visible: inspectorOpen,
        open: pane ? pane.tabs.map((paneTab) => paneTab.kind) : [],
        active: pane?.tabs.find((paneTab) => paneTab.id === pane.activeId)?.kind ?? null,
      });
    };
    window.addEventListener(OPEN_PANE_TAB_EVENT, open);
    window.addEventListener(SET_PANE_VISIBLE_EVENT, setVisible);
    window.addEventListener(GET_PANE_STATE_EVENT, state);
    return () => {
      window.removeEventListener(OPEN_PANE_TAB_EVENT, open);
      window.removeEventListener(SET_PANE_VISIBLE_EVENT, setVisible);
      window.removeEventListener(GET_PANE_STATE_EVENT, state);
    };
  }, [openTabHere, changeInspector, inspectorOpen, tabId]);

  useEffect(() => {
    const handle = (event: KeyboardEvent) => {
      // The palette, the sidebar and pinned conversations live in the shell; keys pressed in the frame go there.
      const shortcut = shellShortcutFromEvent(event);
      if (shortcut && shellShortcuts.includes(shortcutId(shortcut))) {
        window.parent.postMessage(
          { type: SHORTCUT_MESSAGE, key: shortcut.key, shift: shortcut.shift },
          location.origin,
        );
        event.preventDefault();
        event.stopPropagation();
        return;
      }
      const paneKind = paneShortcutFromEvent(event);
      if (paneKind) {
        event.preventDefault();
        event.stopPropagation();
        openPaneTab(paneKind);
        return;
      }
      if (!(event.metaKey || event.ctrlKey) || event.altKey || event.shiftKey) return;
      if (event.key.toLowerCase() === 'j') changeNavigator(!navigatorOpen);
      else if (event.key === '.') changeInspector(!inspectorOpen);
      else return;
      event.preventDefault();
      event.stopPropagation();
    };
    window.addEventListener('keydown', handle, true);
    return () => window.removeEventListener('keydown', handle, true);
  }, [navigatorOpen, inspectorOpen, changeNavigator, changeInspector, shellShortcuts]);

  return (
    <RuntimeMachineContext.Provider value={runtimeMachineId ?? undefined}>
      <SWRConfig
        value={{
          fallback: { '/api/quick-prompts': initialQuickPrompts, '/api/sidebar-items': initialSidebarItems },
        }}
      >
        <div
          className="tw-root tw-runtime-root"
          data-split={split ? '' : undefined}
          style={
            {
              '--tw-right-w': `${split?.right ?? width}px`,
              '--tw-frame-top': `${split?.top ?? 0}px`,
            } as React.CSSProperties
          }
        >
          <main className="tw-main" data-pane={inspectorOpen && !paneOverlay ? '' : undefined}>
            <div className="tw-surface">
              <span ref={summaryAnchor} className="tw-rpane-summary-anchor" aria-hidden />
              {error && (
                <div className="tw-error tw-error-floating" role="alert">
                  {error}
                </div>
              )}
              <section className="tw-content">
                <div className="tw-pane">
                  <TerminalSurfaceVisibleContext.Provider value={frameVisible}>
                    <TerminalPage />
                  </TerminalSurfaceVisibleContext.Provider>
                </div>
              </section>
            </div>
            {inspectorOpen && (
              <>
                <button
                  type="button"
                  className="tw-scrim tw-inspector-scrim"
                  aria-label={t('agents.runtimeWorkspace.closePane')}
                  onClick={() => changeInspector(false)}
                />
                <div className="tw-inspector-host" data-maximised={maximised || undefined}>
                  {!isMobile && !maximised && (
                    <ColumnResizer
                      side="right"
                      width={split?.right ?? width}
                      min={PANE_MIN}
                      max={paneMax}
                      defaultWidth={PANE_DEFAULT}
                      cssVar="--tw-right-w"
                      onCommit={commitWidth}
                    />
                  )}
                  <ConversationPane
                    mode="agent"
                    tab={activeTab}
                    chatId={null}
                    provider={provider}
                    maximised={maximised && !paneOverlay}
                    onToggleMaximised={() => setMaximised((value) => !value)}
                    onHide={() => changeInspector(false)}
                  />
                </div>
              </>
            )}
          </main>
          <FloatingPreview tab={activeTab} onDock={() => openTabHere({ kind: 'browser' })} />
          <PaneSummary
            open={summaryOpen}
            onClose={() => setSummaryOpen(false)}
            anchorRef={summaryAnchor}
            mode="agent"
            tab={activeTab}
            chatId={null}
          />
          <VaultAgentRequests tabId={tabId} />
        </div>
        <ConversationNavigator
          open={navigatorOpen}
          onOpenChange={changeNavigator}
          snapshotKey={activeTab?.sessionName ?? null}
        />
      </SWRConfig>
    </RuntimeMachineContext.Provider>
  );
};

export default RuntimeWorkspace;
