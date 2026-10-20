import useBoardFrame from '@/hooks/use-board-frame';
import { gitlabHosts } from '@/lib/site';
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import { ExternalLink, Maximize2, Minimize2, PanelRightClose, Plus, Shapes, X } from 'lucide-react';
import { cx } from '@/components/agents/ui';
import DocViewer from '@/components/agents/docs/doc-viewer';
import SubagentsTab from '@/components/agents/inspector/subagents-tab';
import ArtifactsTab from '@/components/agents/inspector/artifacts-tab';
import PeersTab from '@/components/agents/inspector/peers-tab';
import GitLabTab from '@/components/agents/inspector/gitlab-tab';
import ChangesTab, { CommitView, isCommitTarget } from '@/components/agents/inspector/changes-tab';
import GitLabView from '@/components/agents/inspector/gitlab-view';
import FilesTab from '@/components/agents/inspector/files-tab';
import DiskTab from '@/components/agents/inspector/disk-tab';
import PlanTab from '@/components/agents/inspector/plan-tab';
import FileViewer, { FileViewerContent } from '@/components/agents/inspector/file-viewer';
import * as EconomicsIsland from '@/components/agents/inspector/economics-island';
import BrowserTab, { shownAddress } from '@/components/agents/pane/browser-tab';
import ShellTab, { closePaneShell } from '@/components/agents/pane/shell-tab';
import ToolsPage, { type IToolCount } from '@/components/agents/pane/tools-page';
import OverviewTab, { type IOverviewIsland } from '@/components/agents/pane/overview-tab';
import TaskTab from '@/components/agents/pane/task-tab';
import DesignLibraryTab, { DesignIsland } from '@/components/agents/pane/design-library-tab';
import SiteIcon from '@/components/agents/site-icon';
import SideChatView from '@/components/agents/side-chat/side-chat-view';
import GalleryView from '@/components/agents/gallery/gallery-view';
import VaultTab from '@/components/agents/vault/vault-tab';
import { PANE_KINDS, TOOL_ORDER } from '@/components/agents/pane/pane-registry';
import useLinkMeta from '@/hooks/use-link-meta';
import usePaneData from '@/hooks/use-pane-data';
import usePaneArrivals from '@/hooks/use-pane-arrivals';
import { ARRIVAL_REVEAL_MAX_MS, ARRIVAL_SETTLE_MS, hasArrivals } from '@/lib/hl-pane-arrival';
import useDesignLibrary from '@/hooks/use-design-library';
import { groupArtifacts } from '@/lib/hl-design-library/group';
import { subagentActivityKey } from '@/lib/hl-activity-signals';
import { fileKindOf, type IViewableFile } from '@/lib/hl-artifacts';
import { boardFrameUrlOf, isPaneDocPath } from '@/lib/hl-doc-path';
import { gitlabRefOf } from '@/lib/hl-gitlab-ref';
import { paneRequestOf } from '@/lib/hl-pane-link';
import { paneKeyOf, usePane } from '@/lib/hl-pane-store';
import { PaneSurfaceShownContext } from '@/hooks/use-pane-surface-shown';
import {
  OVERVIEW_TAB_ID,
  activateTab,
  closeTab,
  openTab,
  retitleTab,
  stepTab,
  type IPaneTab,
  type TPaneTabKind,
} from '@/lib/hl-pane-tabs';
import { usePreviewState } from '@/lib/hl-preview-store';
import type { ITab } from '@/types/terminal';
import { t } from '@/lib/i18n';

/**
 * Tabs whose state cannot be rebuilt cheaply (a page, a shell, a chat) stay mounted while hidden;
 * the rest render only in front. A hidden diff or merge-request list re-rendered with every conversation switch.
 */
const KEEP_ALIVE: ReadonlySet<TPaneTabKind> = new Set<TPaneTabKind>([
  'browser',
  'terminal',
  'side-chat',
  'design',
]);

const baseName = (path: string): string => path.replace(/\/+$/, '').split('/').at(-1) || path;

/** The islands of the overview, in the order the panel had them before it got tabs. */
const ISLAND_ORDER: readonly TPaneTabKind[] = [
  'economics',
  'design',
  'subagents',
  'plan',
  'artifacts',
  'gitlab',
  'files',
  'disk',
  'peers',
  'changes',
];

const GitLabIcon = () => <SiteIcon href={`https://${gitlabHosts()[0] ?? 'gitlab.com'}`} />;

/** Islands a fresh agent conversation shows before it has anything of its own: they need no tab to be drawn. */
const LAUNCH_ISLANDS: ReadonlySet<TPaneTabKind> = new Set(['disk']);

interface IConversationPaneProps {
  mode: 'agent' | 'chat';
  tab: ITab | null;
  chatId: string | null;
  provider: string;
  maximised: boolean;
  onToggleMaximised: () => void;
  onHide: () => void;
  /**
   * The agent conversation is being created and has no tab yet: the panel shows the islands a fresh
   * conversation starts with, so it does not flash an empty overview for the second before the tab arrives.
   */
  launching?: boolean;
}

interface IDocTabProps {
  /** The reader's path of the document (`/agents/docs/<slug>`). */
  path: string;
  /** The tab is the one in front: a viewer behind another tab asks the portal for nothing. */
  active: boolean;
  onTitle: (title: string) => void;
}

/**
 * A document or a board as a tab: the viewer itself, drawn by the panel, with its comments and versions.
 * Not a frame with the reader's page in it, which brought the platform's own bar and its back-to-conversations link into the panel.
 */
const DocTab = ({ path, active, onTitle }: IDocTabProps) => {
  const [version, setVersion] = useState<string | null>(null);
  return (
    <DocViewer
      slug={baseName(path)}
      embedded
      visible={active}
      versionSha={version}
      onVersionChange={setVersion}
      onTitle={onTitle}
    />
  );
};

interface IBoardTabProps {
  /** `boardFrameUrlOf`: the board's own address with its key. */
  url: string;
  title: string;
  onTitle: (title: string) => void;
}

/**
 * A board as a tab: the board itself by the address the conversation gave, key and all. It does not go
 * through the reader, which knows only boards bound to a conversation; the board lets this platform frame it.
 */
const BoardTab = ({ url, title, onTitle }: IBoardTabProps) => {
  const meta = useLinkMeta(url);
  const known = meta?.kind === 'artifact' && meta.title ? meta.title : null;
  useEffect(() => {
    if (known) onTitle(known);
  }, [known, onTitle]);
  const shown = known ?? title;
  const src = useBoardFrame(url);
  return (
    <div className="td-app" data-embedded>
      <header className="td-bar">
        <span className="td-bar-kind" aria-hidden="true">
          <Shapes />
        </span>
        <div className="td-bar-title">
          <h1 title={shown}>{shown}</h1>
        </div>
        <div className="td-bar-actions">
          <a
            className="td-icon"
            href={url}
            target="_blank"
            rel="noreferrer"
            title={t('pane.conversationPane.openInBrowser')}
            aria-label={t('pane.conversationPane.openInBrowser')}
          >
            <ExternalLink />
          </a>
        </div>
      </header>
      <div className="td-body">
        <main className="td-canvas">
          {src && (
            <iframe
              className="td-board"
              data-testid="board-frame"
              src={src}
              title={shown}
              allow="clipboard-read; clipboard-write; fullscreen"
              referrerPolicy="no-referrer"
            />
          )}
        </main>
      </div>
    </div>
  );
};

const BrowserTitle = ({ previewKey }: { previewKey: string }) => {
  const [{ state }] = usePreviewState(previewKey);
  return <>{shownAddress(state) || PANE_KINDS.browser.title}</>;
};

interface ITabTitleProps {
  paneTab: IPaneTab;
  tab: ITab | null;
  cwd: string | null;
}

const TabTitle = ({ paneTab, tab, cwd }: ITabTitleProps) => {
  if (paneTab.kind === 'browser' && tab) return <BrowserTitle previewKey={tab.id} />;
  if (paneTab.kind === 'terminal' && cwd) return <>{baseName(cwd)}</>;
  // A board's target is its address with the key in it: never a name to show.
  if (paneTab.kind === 'artifacts' && paneTab.target && boardFrameUrlOf(paneTab.target))
    return <>{paneTab.title ?? t('pane.conversationPane.board')}</>;
  // A tab an agent asked for comes without a name: a merge request or a commit is named after what its address says.
  if (paneTab.target)
    return <>{paneTab.title ?? paneRequestOf(paneTab.target)?.title ?? baseName(paneTab.target)}</>;
  return <>{PANE_KINDS[paneTab.kind].title}</>;
};

/**
 * The right panel of a conversation as in Codex desktop: a row of tabs with «+», «expand» and «hide»,
 * the new tab with the address field and the tools, and each former island as a tab of its own. The first tab is the
 * overview with the islands themselves; it stays mounted, so their counts keep up behind any other tab.
 */
const ConversationPane = ({
  mode,
  tab,
  chatId,
  provider,
  maximised,
  onToggleMaximised,
  onHide,
  launching = false,
}: IConversationPaneProps) => {
  const agent = mode === 'agent';
  const paneKey = paneKeyOf(mode, tab?.id ?? null, chatId);
  const [pane, update] = usePane(paneKey);
  const data = usePaneData(mode, tab, chatId);
  const { sessionName, jsonlPath, cwd, entries, tasks, links, workPaths, peerMessages, files } = data;
  const tabId = tab?.id ?? null;
  const idBase = useId();
  // A panel mounted for a tab the agent brought shows its skeleton while it slides in: mounting a viewer in
  // the frames of the movement stalled it for a tenth of a second.
  const [arriving] = useState(() => paneKey !== null && hasArrivals(paneKey));
  const [contentsReady, setContentsReady] = useState(!arriving);
  useEffect(() => {
    if (contentsReady) return;
    const timer = window.setTimeout(() => setContentsReady(true), ARRIVAL_SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, [contentsReady]);
  const paneRef = useRef<HTMLElement>(null);
  // Once mounted, the contents stay under the skeleton until they have something to show (a viewer fetches its file, a
  // document its page), then the skeleton dissolves into them: the panel is never an empty dark box in between.
  const [skeletonDone, setSkeletonDone] = useState(!arriving);
  useEffect(() => {
    const panel = paneRef.current?.querySelector<HTMLElement>('.tw-rpane-panel:not([hidden])');
    if (!contentsReady || skeletonDone || !panel) return;
    const skeleton = panel.querySelector<HTMLElement>(':scope > .tw-rpane-arrival-skeleton');
    let done = false;
    const reveal = () => {
      if (done) return;
      done = true;
      observer.disconnect();
      window.clearTimeout(timer);
      const fade = skeleton?.animate([{ opacity: 1 }, { opacity: 0 }], {
        duration: 260,
        easing: 'ease-out',
        fill: 'forwards',
      });
      for (const child of panel.children) {
        if (child !== skeleton)
          child.animate(
            [
              { opacity: 0, transform: 'translateY(8px)' },
              { opacity: 1, transform: 'none' },
            ],
            { duration: 420, easing: 'cubic-bezier(0.16, 1, 0.3, 1)' },
          );
      }
      if (fade) fade.onfinish = () => setSkeletonDone(true);
      else setSkeletonDone(true);
    };
    const filled = () =>
      [...panel.children].some(
        (child) =>
          child !== skeleton &&
          ((child.textContent ?? '').trim().length > 24 ||
            child.querySelector('img, svg, canvas, iframe, video, table, pre') !== null),
      );
    const observer = new MutationObserver(() => {
      if (filled()) reveal();
    });
    observer.observe(panel, { childList: true, subtree: true, characterData: true });
    const timer = window.setTimeout(reveal, ARRIVAL_REVEAL_MAX_MS);
    if (filled()) reveal();
    return () => {
      observer.disconnect();
      window.clearTimeout(timer);
    };
  }, [contentsReady, skeletonDone]);

  const [subagents, setSubagents] = useState({ running: 0, total: 0 });
  const onSubagents = useCallback((running: number, total: number) => setSubagents({ running, total }), []);
  const [gitlab, setGitlab] = useState({ count: 0, live: false });
  const onGitlab = useCallback((count: number, live: boolean) => setGitlab({ count, live }), []);
  const [changeCount, setChangeCount] = useState(0);
  const subagentActivity = useMemo(() => subagentActivityKey(entries), [entries]);
  const mentionedMrs = useMemo(
    () => links.filter((link) => link.group === 'mr').map((link) => link.url),
    [links],
  );
  const economicsVisible = EconomicsIsland.useIslandVisible(tab, provider);
  const activeKind = pane.tabs.find((paneTab) => paneTab.id === pane.activeId)?.kind;
  const designLibrary = useDesignLibrary(
    tabId,
    agent && !!tab,
    activeKind === 'overview' || activeKind === 'design',
  );
  const designArtifacts = useMemo(
    () => (designLibrary.state.kind === 'ready' ? designLibrary.state.data.artifacts : []),
    [designLibrary.state],
  );
  const designMine = useMemo(() => groupArtifacts(designArtifacts).mine, [designArtifacts]);

  const available = (kind: TPaneTabKind): boolean => {
    if (PANE_KINDS[kind].agentOnly && !(agent && tab)) return agent && launching && LAUNCH_ISLANDS.has(kind);
    if (kind === 'changes') return !!sessionName;
    if (kind === 'economics') return economicsVisible;
    return true;
  };
  const tools = TOOL_ORDER.filter(available);
  const tasksLeft = tasks.filter((task) => task.status !== 'completed').length;
  const counts: Partial<Record<TPaneTabKind, IToolCount>> = {
    subagents: { value: subagents.running || subagents.total, live: subagents.running > 0 },
    plan: { value: tasksLeft || tasks.length, live: tasksLeft > 0 },
    artifacts: { value: links.filter((link) => link.group !== 'mr').length },
    gitlab: { value: gitlab.count, live: gitlab.live },
    changes: { value: changeCount },
    files: { value: files.length },
    peers: { value: peerMessages.length },
    design: {
      value: designMine.length,
      live: designMine.some((artifact) => artifact.agents.some((item) => item.self && item.state === 'busy')),
    },
  };

  const open = (
    kind: TPaneTabKind,
    target: string | null = null,
    title: string | null = null,
    replace?: string,
  ) => update((state) => openTab(state, kind, target, { title, replace }));
  const close = (paneTab: IPaneTab) => {
    if (paneTab.kind === 'terminal' && tabId) closePaneShell(tabId);
    update((state) => closeTab(state, paneTab.id));
  };
  // A click on a file is the window over the conversation, as before the tabs; the tab is the second way in.
  const [viewing, setViewing] = useState<IViewableFile | null>(null);
  const openFileTab = (file: IViewableFile) => open('files', file.path, baseName(file.path));
  const openDoc = (path: string, title: string) => open('artifacts', path, title);
  const openCommit = (target: string, title: string) => open('changes', target, title);
  const openMr = (url: string, title: string) => open('gitlab', url, title);
  const openTask = (key: string, title: string | null) => open('task', key, title);
  const openBoard = (address: string, title: string) => open('browser', address, title);

  const tabIdOf = (paneTab: IPaneTab) => `${idBase}-tab-${paneTab.id}`;
  const panelIdOf = (paneTab: IPaneTab) => `${idBase}-panel-${paneTab.id}`;
  const focusTab = (id: string) =>
    window.requestAnimationFrame(() => document.getElementById(`${idBase}-tab-${id}`)?.focus());
  const onTabKey = (event: KeyboardEvent<HTMLButtonElement>, paneTab: IPaneTab) => {
    const { tabs } = pane;
    let next: string | null = null;
    if (event.key === 'ArrowRight') next = stepTab(pane, 1).activeId;
    else if (event.key === 'ArrowLeft') next = stepTab(pane, -1).activeId;
    else if (event.key === 'Home') next = tabs[0].id;
    else if (event.key === 'End') next = tabs[tabs.length - 1].id;
    else if (event.key === 'Delete') {
      if (paneTab.id === OVERVIEW_TAB_ID) return;
      event.preventDefault();
      const after = closeTab(pane, paneTab.id);
      close(paneTab);
      focusTab(after.activeId);
      return;
    } else return;
    event.preventDefault();
    update((state) => activateTab(state, next));
    focusTab(next);
  };

  const listOf = (kind: TPaneTabKind): ReactNode => content({ id: kind, kind, target: null, title: null });
  const islandBody = (kind: TPaneTabKind): ReactNode => {
    if (kind === 'economics')
      return <EconomicsIsland.Island tab={tab} provider={provider} expanded={false} />;
    if (kind === 'design')
      return (
        <DesignIsland library={designLibrary} onOpenBoard={openBoard} onOpenTab={() => open('design')} />
      );
    if (kind === 'disk') return <p className="tw-rpane-muted">{t('pane.conversationPane.diskHint')}</p>;
    return listOf(kind);
  };
  const linkCount = links.filter((link) => link.group !== 'mr').length;
  const islandVisible: Partial<Record<TPaneTabKind, boolean>> = {
    economics: economicsVisible,
    design: designArtifacts.length > 0,
    subagents: subagents.total > 0,
    plan: tasks.length > 0,
    artifacts: linkCount > 0,
    gitlab: gitlab.count > 0,
    files: files.length > 0,
    disk: true,
    peers: peerMessages.length > 0,
    changes: changeCount > 0,
  };
  const islands = (): IOverviewIsland[] =>
    ISLAND_ORDER.filter(available).map((kind) => ({
      kind,
      visible: !!islandVisible[kind],
      count: counts[kind]?.value,
      live: counts[kind]?.live,
      icon: kind === 'gitlab' ? GitLabIcon : undefined,
      body: islandBody(kind),
    }));

  const content = (paneTab: IPaneTab): ReactNode => {
    if (!available(paneTab.kind) && paneTab.kind !== 'tools' && paneTab.kind !== 'overview')
      return <p className="tw-rpane-muted">{t('pane.conversationPane.unavailable')}</p>;
    switch (paneTab.kind) {
      case 'overview':
        return (
          <OverviewTab
            islands={islands()}
            onOpenTab={(kind) => open(kind)}
            withServers={agent && (!!tab || launching)}
          />
        );
      case 'tools':
        return (
          <ToolsPage
            tools={tools}
            counts={counts}
            onAddress={agent && tab ? (address) => open('browser', address, null, paneTab.id) : null}
            onOpen={(kind, beside) => open(kind, null, null, beside ? undefined : paneTab.id)}
          />
        );
      case 'browser':
        return (
          tab && (
            <BrowserTab
              tab={tab}
              entries={entries}
              workPaths={workPaths}
              cwd={cwd}
              target={paneTab.target}
              request={paneTab.asked}
              onTarget={(address) => open('browser', address)}
            />
          )
        );
      case 'terminal':
        return tabId && <ShellTab tabId={tabId} active={pane.activeId === paneTab.id} />;
      case 'files': {
        if (!paneTab.target) return <FilesTab files={files} onOpen={setViewing} onOpenInPane={openFileTab} />;
        const file = files.find((item) => item.path === paneTab.target) ?? {
          path: paneTab.target,
          kind: fileKindOf(paneTab.target),
          origin: 'mentioned' as const,
          timestamp: 0,
        };
        return (
          <FileViewerContent
            file={file}
            files={files}
            onNavigate={(next) =>
              update((state) =>
                closeTab(openTab(state, 'files', next.path, { title: baseName(next.path) }), paneTab.id),
              )
            }
            heading={(name) => (
              <h3 className="tw-rpane-file-name" title={name}>
                {name}
              </h3>
            )}
          />
        );
      }
      case 'disk':
        return <DiskTab cwd={cwd} />;
      case 'artifacts':
        if (!paneTab.target) return <ArtifactsTab links={links} onOpenDoc={openDoc} />;
        if (boardFrameUrlOf(paneTab.target) === paneTab.target) {
          return (
            <BoardTab
              key={paneTab.target}
              url={paneTab.target}
              title={paneTab.title ?? t('pane.conversationPane.board')}
              onTitle={(title) => update((state) => retitleTab(state, paneTab.id, title))}
            />
          );
        }
        return isPaneDocPath(paneTab.target) ? (
          <DocTab
            key={paneTab.target}
            path={paneTab.target}
            active={pane.activeId === paneTab.id}
            onTitle={(title) => update((state) => retitleTab(state, paneTab.id, title))}
          />
        ) : (
          <p className="tw-rpane-muted">{t('pane.conversationPane.unsupportedAddress')}</p>
        );
      case 'changes':
        if (!sessionName) return null;
        return paneTab.target && isCommitTarget(paneTab.target) ? (
          <CommitView target={paneTab.target} title={paneTab.title} sessionName={sessionName} tabId={tabId} />
        ) : (
          <ChangesTab
            paths={workPaths}
            sessionName={sessionName}
            tabId={tabId}
            jsonlPath={jsonlPath}
            onCount={setChangeCount}
            onOpenCommit={openCommit}
          />
        );
      case 'gitlab':
        return paneTab.target && gitlabRefOf(paneTab.target) ? (
          <GitLabView url={paneTab.target} sessionName={sessionName} tabId={tabId} />
        ) : (
          <GitLabTab
            paths={workPaths}
            jsonlPath={jsonlPath}
            mentioned={mentionedMrs}
            onCount={onGitlab}
            onOpenMr={openMr}
          />
        );
      case 'subagents':
        return (
          <SubagentsTab
            key={jsonlPath ?? 'none'}
            jsonlPath={jsonlPath}
            provider={provider}
            activity={subagentActivity}
            onCount={onSubagents}
          />
        );
      case 'plan':
        return tasks.length > 0 ? (
          <PlanTab tasks={tasks} />
        ) : (
          <p className="tw-rpane-muted">{t('pane.conversationPane.noPlan')}</p>
        );
      case 'peers':
        return <PeersTab messages={peerMessages} />;
      case 'economics':
        return <EconomicsIsland.Island tab={tab} provider={provider} expanded />;
      case 'design':
        return <DesignLibraryTab library={designLibrary} onOpenBoard={openBoard} />;
      case 'side-chat':
        return <SideChatView tabId={tabId} sessionName={sessionName} provider={provider} />;
      case 'gallery':
        return <GalleryView entries={entries} tabId={tabId} />;
      case 'task':
        return <TaskTab taskKey={paneTab.target} onOpenTask={openTask} />;
      case 'vault':
        return <VaultTab />;
    }
  };

  useEffect(() => {
    document
      .getElementById(`${idBase}-tab-${pane.activeId}`)
      ?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }, [idBase, pane.activeId]);

  const stripRef = useRef<HTMLDivElement>(null);
  usePaneArrivals(paneKey, pane.tabs, pane.activeId, stripRef, idBase);

  return (
    <section
      ref={paneRef}
      className="tw-rpane"
      data-testid="right-pane"
      data-maximised={maximised || undefined}
      aria-label={t('pane.conversationPane.panel')}
    >
      <header className="tw-rpane-strip">
        <span className="tw-rpane-edge" aria-hidden />
        <div
          ref={stripRef}
          className="tw-rpane-tabs"
          role="tablist"
          aria-label={t('pane.conversationPane.tabs')}
        >
          {pane.tabs.map((paneTab) => {
            const { icon: Icon } = PANE_KINDS[paneTab.kind];
            const selected = paneTab.id === pane.activeId;
            const pinned = paneTab.id === OVERVIEW_TAB_ID;
            return (
              <div
                key={paneTab.id}
                className="tw-rpane-tab"
                data-tab-id={paneTab.id}
                data-active={selected || undefined}
                data-pinned={pinned || undefined}
                data-unread={paneTab.unread || undefined}
                role="presentation"
              >
                <span className="tw-rpane-tab-glow" aria-hidden />
                <span className="tw-rpane-tab-light" aria-hidden />
                <button
                  type="button"
                  role="tab"
                  id={tabIdOf(paneTab)}
                  aria-selected={selected}
                  aria-controls={panelIdOf(paneTab)}
                  tabIndex={selected ? 0 : -1}
                  className="tw-rpane-tab-button"
                  onClick={() => update((state) => activateTab(state, paneTab.id))}
                  onKeyDown={(event) => onTabKey(event, paneTab)}
                >
                  <Icon />
                  <span className="tw-rpane-tab-title">
                    <TabTitle paneTab={paneTab} tab={tab} cwd={cwd} />
                  </span>
                  {paneTab.unread && (
                    <span
                      className="tw-rpane-tab-unread"
                      role="img"
                      aria-label={t('pane.conversationPane.unread')}
                    />
                  )}
                </button>
                {!pinned && (
                  <button
                    type="button"
                    className="tw-rpane-tab-close"
                    tabIndex={-1}
                    aria-label={t('pane.conversationPane.closeTab')}
                    title={t('pane.conversationPane.closeTabHint')}
                    onClick={() => close(paneTab)}
                  >
                    <X />
                  </button>
                )}
              </div>
            );
          })}
        </div>
        <button
          type="button"
          className="tw-icon-button tw-rpane-new"
          aria-label={t('pane.conversationPane.newTab')}
          title={t('pane.conversationPane.newTab')}
          onClick={() => open('tools')}
        >
          <Plus />
        </button>
        <span className="tw-rpane-strip-actions">
          <button
            type="button"
            className="tw-icon-button tw-rpane-maximise"
            aria-label={maximised ? t('pane.conversationPane.restore') : t('pane.conversationPane.maximise')}
            aria-pressed={maximised}
            title={maximised ? t('pane.conversationPane.restoreHint') : t('pane.conversationPane.maximise')}
            onClick={onToggleMaximised}
          >
            {maximised ? <Minimize2 /> : <Maximize2 />}
          </button>
          <button
            type="button"
            className="tw-icon-button"
            data-testid="right-pane-hide"
            aria-label={t('pane.conversationPane.hide')}
            title={t('pane.conversationPane.hideHint')}
            onClick={onHide}
          >
            <PanelRightClose />
          </button>
        </span>
      </header>
      {pane.tabs.map((paneTab) => (
        <div
          key={paneTab.id}
          role="tabpanel"
          id={panelIdOf(paneTab)}
          aria-labelledby={tabIdOf(paneTab)}
          className={cx('tw-rpane-panel', `tw-rpane-panel-${paneTab.kind}`)}
          data-arriving={(paneTab.id === pane.activeId && !skeletonDone) || undefined}
          hidden={paneTab.id !== pane.activeId}
        >
          {(paneTab.id === pane.activeId || KEEP_ALIVE.has(paneTab.kind)) && contentsReady && (
            <PaneSurfaceShownContext.Provider value={paneTab.id === pane.activeId}>
              {content(paneTab)}
            </PaneSurfaceShownContext.Provider>
          )}
          {paneTab.id === pane.activeId && !skeletonDone && (
            <div className="tw-rpane-arrival-skeleton" data-over={contentsReady || undefined} aria-hidden>
              <span className="tw-skeleton" />
              <span className="tw-skeleton" />
              <span className="tw-skeleton" />
              <span className="tw-skeleton" />
            </div>
          )}
        </div>
      ))}
      <FileViewer
        file={viewing}
        files={files}
        onNavigate={setViewing}
        onClose={() => setViewing(null)}
        onOpenInPane={openFileTab}
      />
    </section>
  );
};

export default ConversationPane;
