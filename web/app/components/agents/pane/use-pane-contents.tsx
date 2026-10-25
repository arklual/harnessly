import useBoardFrame from '@/hooks/use-board-frame';
import { gitlabHosts } from '@/lib/site';
import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { ExternalLink, Shapes } from 'lucide-react';

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
import { FileViewerContent } from '@/components/agents/inspector/file-viewer';
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

import useDesignLibrary from '@/hooks/use-design-library';
import { groupArtifacts } from '@/lib/hl-design-library/group';
import { subagentActivityKey } from '@/lib/hl-activity-signals';
import { fileKindOf, type IViewableFile } from '@/lib/hl-artifacts';
import { boardFrameUrlOf, isPaneDocPath } from '@/lib/hl-doc-path';
import { gitlabRefOf } from '@/lib/hl-gitlab-ref';
import { paneRequestOf } from '@/lib/hl-pane-link';
import { paneKeyOf, usePane } from '@/lib/hl-pane-store';

import { closeTab, openTab, retitleTab, type IPaneTab, type TPaneTabKind } from '@/lib/hl-pane-tabs';
import { usePreviewState } from '@/lib/hl-preview-store';
import type { ITab } from '@/types/terminal';
import { t } from '@/lib/i18n';

// What the right pane of a conversation shows in each of its tabs, and the state behind them (`hl-pane-store`): one
// place for both interfaces. The current one draws these tabs in its own strip; the Legacy one in `ConversationPane`.

/**
 * Tabs whose state cannot be rebuilt cheaply (a page, a shell, a chat) stay mounted while hidden;
 * the rest render only in front. A hidden diff or merge-request list re-rendered with every conversation switch.
 */
export const KEEP_ALIVE: ReadonlySet<TPaneTabKind> = new Set<TPaneTabKind>([
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

export const TabTitle = ({ paneTab, tab, cwd }: ITabTitleProps) => {
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

export interface IPaneContentsOptions {
  mode: 'agent' | 'chat';
  tab: ITab | null;
  chatId: string | null;
  provider: string;
  /** The agent conversation is being created and has no tab yet (see `ConversationPane`). */
  launching?: boolean;
}

export const usePaneContents = ({ mode, tab, chatId, provider, launching = false }: IPaneContentsOptions) => {
  const agent = mode === 'agent';
  const paneKey = paneKeyOf(mode, tab?.id ?? null, chatId);
  const [pane, update] = usePane(paneKey);
  const data = usePaneData(mode, tab, chatId);
  const { sessionName, jsonlPath, cwd, entries, tasks, links, workPaths, peerMessages, files } = data;
  const tabId = tab?.id ?? null;
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

  return {
    agent,
    paneKey,
    pane,
    update,
    data,
    tabId,
    tools,
    counts,
    available,
    open,
    close,
    content,
    viewing,
    setViewing,
    openFileTab,
  };
};

export type TPaneContents = ReturnType<typeof usePaneContents>;
