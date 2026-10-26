// The bridge to the Legacy interface: the only place of app/next that imports it (eslint.config.js). The pane's tools
// Legacy already has (artifacts, diff, merge requests, subagents, spend, agents' correspondence, gallery, the overview
// with its islands) are drawn here with Legacy's contents (`usePaneContents`), over the Legacy tab the conversation
// runs in. What the agent asks its pane to show (`harnessly open`, the runtime's own auto-open) is the new pane's
// (bindings/pane-intents.ts): it opens these tools through `show`. When Legacy goes, these tools are written for the new
// interface and this folder goes with it.
import { Suspense, use, useEffect, useEffectEvent, useMemo, useRef, useState, type ReactNode } from 'react';
import useTimeline from '@/hooks/use-timeline';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { PaneSurfaceShownContext } from '@/hooks/use-pane-surface-shown';
import { usePaneContents } from '@/components/agents/pane/use-pane-contents';
import { PANE_KINDS } from '@/components/agents/pane/pane-registry';
import FileViewer from '@/components/agents/inspector/file-viewer';
import { updatePane } from '@/lib/hl-pane-store';
import {
  activateTab,
  closeTab,
  openTab,
  OVERVIEW_TAB_ID,
  paneTabId,
  type IPaneTab,
  type TPaneTabKind,
} from '@/lib/hl-pane-tabs';
import type { ITab } from '@/types/terminal';
import { loadLegacyTab } from '@/next/commands/pages';
import ArtifactsView from '@/next/features/artifacts/artifacts-view';
import { changeLookOf } from '@/next/legacy/change-tab';
import { artifactItemOf, artifactRequestOf } from '@/next/legacy/artifacts';
import { useLinkRestored } from '@/next/stores/connection';
import type { Conversation, LegacyTab } from '@/next/protocol/v2';

/** The tools of the launcher that come from Legacy, with the design's group of each: the chat's, or a service. The
 *  overview and Claude Design are not among them: the design has no overview, and Designly takes Claude Design's place.
 *  The subagents are the pane's own. */
const TOOLS = [
  { kind: 'changes', group: 'chat' },
  { kind: 'gallery', group: 'chat' },
  { kind: 'artifacts', group: 'chat' },
  { kind: 'peers', group: 'chat' },
  { kind: 'economics', group: 'chat' },
  { kind: 'disk', group: 'vm' },
  { kind: 'gitlab', group: 'links' },
  { kind: 'vault', group: 'links' },
] as const satisfies readonly { kind: TPaneTabKind; group: string }[];

/** Legacy's tabs the design has no place for: one left open in Legacy does not come over. */
const GONE: ReadonlySet<TPaneTabKind> = new Set(['overview', 'design']);

/** Legacy's tabs the new pane has tools of its own for: what Legacy's pane gets for them is handed over. */
const HANDED: ReadonlySet<TPaneTabKind> = new Set(['browser', 'plan', 'side-chat']);

const ICONS: Record<TPaneTabKind, string> = {
  overview: 'layers',
  tools: 'layout-grid',
  browser: 'app-window',
  terminal: 'square-terminal',
  'side-chat': 'message-square-text',
  files: 'file-code',
  disk: 'hard-drive',
  changes: 'git-compare',
  artifacts: 'sparkles',
  gitlab: 'git-pull-request',
  subagents: 'workflow',
  plan: 'list-checks',
  peers: 'message-square',
  economics: 'gauge',
  design: 'palette',
  gallery: 'images',
  task: 'file-text',
  vault: 'key-round',
};

/** A Legacy tab in the new pane's strip is told apart from the pane's own by this prefix. */
const PREFIX = 'L:';
const idOf = (tab: IPaneTab) => `${PREFIX}${tab.id}`;
export const isLegacyTab = (id: string): boolean => id.startsWith(PREFIX);

const baseName = (path: string): string => path.replace(/\/+$/, '').split('/').at(-1) || path;

export interface ILegacyTabInfo {
  id: string;
  icon: string;
  name: string;
  sub: string;
  pinned: boolean;
  /** The agent opened it behind the tab the person was on, and it was not looked at yet. */
  unread: boolean;
}

export interface ILegacyTool {
  id: string;
  icon: string;
  name: string;
  group: 'chat' | 'vm' | 'links';
}

/** What Legacy's pane got for one of the new pane's own tools. */
export type TLegacyHandoff = { kind: 'browser'; target: string | null } | { kind: 'plan' } | { kind: 'side' };

/** The kinds of Legacy's tabs the new pane opens by request (a link of the feed, the agent's open command). */
export type TLegacyShown =
  'task' | 'gitlab' | 'artifacts' | 'changes' | 'economics' | 'peers' | 'gallery' | 'disk' | 'files';

export interface ILegacyPane {
  /** The conversation has a Legacy tab: the tools below work. */
  on: boolean;
  /** The Legacy tab's id: the conversation's pane intents are kept under it on the machine. */
  tabId: string | null;
  tabs: ILegacyTabInfo[];
  tools: ILegacyTool[];
  /** Opens a tool of `tools` and answers the id of its tab. */
  open: (toolId: string) => string;
  /** Opens a task, a merge request, a document or a tool in its own tab, as the agent's open command does, in front or
   *  behind the tab on screen (marked unread); the id of its tab, null while the conversation has no Legacy tab. */
  show: (kind: TLegacyShown, target: string | null, background?: boolean) => string | null;
  /** Reloads the tab that shows this, when there is one: a document published again. */
  reload: (kind: TLegacyShown, target: string | null) => boolean;
  close: (id: string) => void;
  /** The person brought the tab forward: it is read. */
  pick: (id: string) => void;
  render: (id: string) => ReactNode;
  /** What the pane keeps mounted beside its tabs: the timeline the contents read, the file viewer over the chat. */
  extras: ReactNode;
}

const asTab = (conv: Conversation, legacy: LegacyTab): ITab => ({
  id: legacy.tabId,
  sessionName: legacy.sessionName,
  name: conv.title,
  order: 0,
  cwd: legacy.cwd || conv.cwd,
  panelType: conv.provider === 'codex' ? 'codex-cli' : 'claude-code',
  agentState: legacy.agentSessionId
    ? {
        providerId: conv.provider,
        sessionId: legacy.agentSessionId,
        jsonlPath: legacy.jsonlPath,
        summary: null,
      }
    : undefined,
});

/**
 * The Legacy tab of the conversation, asked again while it is not all there: a conversation gets its tab when its agent
 * starts, and its transcript when the agent writes the first line.
 */
const useLegacyTab = (conv: Conversation | null): ITab | null => {
  const [got, setGot] = useState<{ conv: string; legacy: LegacyTab } | null>(null);
  const convId = conv?.id ?? null;
  const machineId = conv?.machineId ?? null;
  const have = got?.conv === convId ? got.legacy : null;
  const settled = have !== null && have.jsonlPath !== null;
  const restored = useLinkRestored(machineId);
  const moved = conv ? `${conv.state.k}|${conv.state.since}|${conv.lastActivityAt}|${restored}` : '';
  useEffect(() => {
    if (!convId || !machineId || settled) return;
    let live = true;
    void loadLegacyTab(machineId, convId).then((res) => {
      if (live && 'ok' in res) setGot({ conv: convId, legacy: res.value });
    });
    return () => {
      live = false;
    };
  }, [convId, machineId, settled, moved]);
  return conv && have ? asTab(conv, have) : null;
};

// The old stylesheets (styles/legacy.ts) come the first time a Legacy content shows, not with the page; what draws
// with them waits for them, so it never shows unstyled.
let legacyStyles: Promise<unknown> | null = null;
const LegacyStyles = ({ need, children }: { need: boolean; children: ReactNode }) => {
  if (need) use((legacyStyles ??= import('@/styles/legacy')));
  return children;
};

/** Keeps the Legacy timeline of the conversation in the snapshot store while mounted: the contents read it there. */
const Timeline = ({ tab, provider }: { tab: ITab; provider: string }) => {
  const timeline = useTimeline({
    sessionName: tab.sessionName,
    agentSessionId: tab.agentState?.sessionId ?? null,
    panelType: provider === 'codex' ? 'codex-cli' : 'claude-code',
  });
  useEffect(() => {
    useTimelineSnapshotStore.getState().publish(tab.sessionName, {
      entries: timeline.entries,
      jsonlPath: timeline.jsonlPath,
      sessionId: timeline.sessionId,
      tasks: timeline.tasks,
    });
  }, [tab.sessionName, timeline.entries, timeline.jsonlPath, timeline.sessionId, timeline.tasks]);
  return null;
};

export interface ILegacyPaneOptions {
  /** The pane is out: the timeline its contents read is kept. */
  open: boolean;
  /** A tab came forward in Legacy's pane (the agent opened it): it comes forward in the new one. `switched`: it is
   *  only the front of the conversation just come to, which the new pane may have kept its own front for. */
  onForward: (id: string, switched: boolean) => void;
  onHandoff: (asked: TLegacyHandoff[]) => void;
}

export const useLegacyPane = (conv: Conversation | null, options: ILegacyPaneOptions): ILegacyPane => {
  const tab = useLegacyTab(conv);
  const contents = usePaneContents({
    mode: 'agent',
    tab,
    chatId: null,
    provider: conv?.provider ?? 'claude',
  });
  const key = tab?.id ?? null;
  // Reloads asked of a tab: its contents mount again.
  const [reloads, setReloads] = useState<Record<string, number>>({});

  const { tabs: all, activeId } = contents.pane;
  // The front tab last heard, and whose: another conversation's front is what it was left on, not one that came forward.
  const seen = useRef<{ key: string | null; active: string | null }>({ key: null, active: null });
  const forward = useEffectEvent((id: string, switched: boolean) => options.onForward(id, switched));
  const handoff = useEffectEvent((asked: TLegacyHandoff[]) => options.onHandoff(asked));
  useEffect(() => {
    if (!key) return;
    const handed = all.filter((x) => HANDED.has(x.kind));
    if (handed.length > 0) {
      updatePane(key, (state) => handed.reduce((acc, x) => closeTab(acc, x.id), state));
      handoff(
        handed.map((x): TLegacyHandoff => {
          if (x.kind === 'browser') return { kind: 'browser', target: x.target };
          return x.kind === 'plan' ? { kind: 'plan' } : { kind: 'side' };
        }),
      );
      return;
    }
    const switched = seen.current.key !== key;
    const moved = switched || activeId !== seen.current.active;
    seen.current = { key, active: activeId };
    if (moved && activeId !== OVERVIEW_TAB_ID && all.some((x) => x.id === activeId))
      forward(`${PREFIX}${activeId}`, switched);
  }, [key, all, activeId]);

  const artifacts = useMemo(() => contents.data.links.map(artifactItemOf), [contents.data.links]);
  const { complete, failed, reload, loaded, jsonlPath } = contents.data;
  // A conversation whose agent has written nothing yet has no transcript to read: its links are the timeline's.
  const noTranscript = loaded && jsonlPath === null && !tab?.agentState?.jsonlPath;

  const shown = tab ? all.filter((x) => x.kind !== 'tools' && !GONE.has(x.kind) && !HANDED.has(x.kind)) : [];
  const find = (id: string) => shown.find((x) => idOf(x) === id);
  return {
    on: tab !== null,
    tabId: key,
    tabs: shown.map((x) => {
      const change = x.kind === 'gitlab' ? changeLookOf(x.target) : null;
      return {
        id: idOf(x),
        icon: change?.icon ?? ICONS[x.kind],
        name: x.title ?? change?.name ?? (x.target ? baseName(x.target) : PANE_KINDS[x.kind].title),
        sub: x.target ?? '',
        pinned: x.id === OVERVIEW_TAB_ID,
        unread: !!x.unread,
      };
    }),
    tools: tab
      ? TOOLS.filter((x) => contents.tools.includes(x.kind)).map((x) => ({
          id: `${PREFIX}${x.kind}`,
          icon: ICONS[x.kind],
          name: PANE_KINDS[x.kind].title,
          group: x.group,
        }))
      : [],
    open: (toolId) => {
      const kind = toolId.slice(PREFIX.length) as TPaneTabKind;
      contents.open(kind);
      return `${PREFIX}${paneTabId(kind, null)}`;
    },
    show: (kind, target, background = false) => {
      if (!tab) return null;
      contents.update((state) => openTab(state, kind, target, { background }));
      return `${PREFIX}${paneTabId(kind, target)}`;
    },
    reload: (kind, target) => {
      const id = paneTabId(kind, target);
      if (!all.some((x) => x.id === id)) return false;
      setReloads((r) => ({ ...r, [id]: (r[id] ?? 0) + 1 }));
      return true;
    },
    close: (id) => {
      const x = find(id);
      if (x) contents.close(x);
    },
    pick: (id) => {
      const x = find(id);
      if (x) contents.update((state) => activateTab(state, x.id));
    },
    render: (id) => {
      const x = find(id);
      // The list of links is the new interface's own; a document, a board or a task opened from it is Legacy's tab.
      if (x?.kind === 'artifacts' && !x.target && conv)
        return (
          <ArtifactsView
            machine={conv.machineId}
            convKey={conv.id}
            items={artifacts}
            complete={complete || noTranscript}
            failed={failed}
            onReload={reload}
            onOpen={(item, title, event) => {
              const request = artifactRequestOf(item, title, event);
              if (!request) return;
              event.preventDefault();
              contents.open(request.kind, request.target, request.title ?? null);
            }}
          />
        );
      return (
        x && (
          <Suspense key={`${x.id}#${reloads[x.id] ?? 0}`} fallback={null}>
            <LegacyStyles need>
              <div className={`hx-legacy tw-rpane-panel tw-rpane-panel-${x.kind}`} data-kind={x.kind}>
                <PaneSurfaceShownContext.Provider value>
                  {contents.content(x)}
                </PaneSurfaceShownContext.Provider>
              </div>
            </LegacyStyles>
          </Suspense>
        )
      );
    },
    extras: (
      <>
        {options.open && tab && conv && <Timeline tab={tab} provider={conv.provider} />}
        <Suspense fallback={null}>
          <LegacyStyles need={contents.viewing !== null}>
            <FileViewer
              file={contents.viewing}
              files={contents.data.files}
              onNavigate={contents.setViewing}
              onClose={() => contents.setViewing(null)}
              onOpenInPane={contents.openFileTab}
            />
          </LegacyStyles>
        </Suspense>
      </>
    ),
  };
};
