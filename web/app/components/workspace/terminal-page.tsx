// The agent conversation in the centre of the page: the open workspace's agent tab the page's address names (else
// the layout's focus), its timeline with the messages just sent, what the agent waits for, the composer and the
// terminal bar; a plain terminal tab is its terminal. Keeps the workspace's layout loaded.
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react';
import TimelineView from '@/components/timeline/timeline-view';
import AgentComposer from '@/components/workspace/agent-composer';
import PendingPrompt from '@/components/workspace/pending-prompt';
import TerminalBar from '@/components/workspace/terminal-bar';
import SessionEnded from '@/components/workspace/session-ended';
import useWorkspaceStore from '@/hooks/use-workspace-store';
import { collectPanes, useLayoutStore } from '@/hooks/use-layout';
import useTabStore, { shouldMarkSeen } from '@/hooks/use-tab-store';
import { dismissTab } from '@/hooks/use-agent-status';
import useTerminalSurfaceVisible from '@/hooks/use-terminal-surface-visible';
import useTimeline from '@/hooks/use-timeline';
import usePendingLaunch from '@/hooks/use-pending-launch';
import useQueueHandoff from '@/hooks/use-queue-handoff';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { pendingUserEntry } from '@/lib/timeline-launch';
import { isRecordedSince } from '@/lib/timeline-pending';
import { sessionMetaFrom } from '@/lib/session-meta';
import { parseAppLocation } from '@/lib/hl-conversation-url';
import { appFetch } from '@/lib/app-path';
import { useRouter } from '@/platform/router';
import { TerminalView } from '@/terminal/terminal-view';
import type { ITab } from '@/types/terminal';
import type { IPendingMessageOptions, ITimelineEntry, ITimelineUserMessage } from '@/types/timeline';

/** A message the composer put up and the time from which the transcript's record of it counts (else: around its own time). */
interface IPendingRow {
  entry: ITimelineUserMessage;
  since: number | null;
}

/** A pending message counts as recorded once the transcript has a message with its text from around its time on. */
const recorded = ({ entry, since }: IPendingRow, entries: readonly ITimelineEntry[]) =>
  since !== null
    ? isRecordedSince(entries, entry, since)
    : entries.some(
        (e) =>
          e.type === 'user-message' &&
          !e.pending &&
          e.text.trim() === entry.text.trim() &&
          e.timestamp >= entry.timestamp - 10_000,
      );

const subscribeVisibility = (onChange: () => void) => {
  document.addEventListener('visibilitychange', onChange);
  return () => document.removeEventListener('visibilitychange', onChange);
};
const pageVisible = () => document.visibilityState === 'visible';

const AgentConversation = ({ tab, workspaceId }: { tab: ITab; workspaceId: string }) => {
  const provider = tab.panelType === 'codex-cli' ? 'codex' : 'claude';
  const status = useTabStore((s) => s.tabs[tab.id]);
  const timeline = useTimeline({
    sessionName: tab.sessionName,
    agentSessionId: status?.agentSessionId ?? tab.agentState?.sessionId ?? null,
    panelType: provider === 'codex' ? 'codex-cli' : 'claude-code',
  });
  const cliState = status?.cliState ?? 'idle';
  const working = cliState === 'busy';
  const [pending, setPending] = useState<IPendingRow[]>([]);
  const [terminalOpen, setTerminalOpen] = useState(false);

  const addPending = useCallback((text: string, options?: IPendingMessageOptions) => {
    const entry = pendingUserEntry(text, options);
    if (!entry) return '';
    setPending((list) => [...list, { entry, since: options?.recordedSince ?? null }]);
    return entry.id;
  }, []);
  const removePending = useCallback(
    (id: string) => setPending((list) => list.filter((row) => row.entry.id !== id)),
    [],
  );
  // A message this page queued and the runtime took out of the queue for the agent shows as a row until the transcript has it.
  useQueueHandoff(tab.id, undefined, addPending, removePending);

  const shownPending = pending.filter((row) => !recorded(row, timeline.entries)).map((row) => row.entry);
  const entries = useMemo(
    () => (shownPending.length ? [...timeline.entries, ...shownPending] : timeline.entries),
    [timeline.entries, shownPending],
  );
  const launch = usePendingLaunch(tab.id, timeline.entries, addPending);

  // The page's other parts (navigator, header economics, gallery) read the conversation from the snapshot store.
  useEffect(() => {
    useTimelineSnapshotStore.getState().publish(tab.sessionName, {
      entries: timeline.entries,
      jsonlPath: timeline.jsonlPath,
      sessionId: timeline.sessionId,
      tasks: timeline.tasks,
    });
  }, [tab.sessionName, timeline.entries, timeline.jsonlPath, timeline.sessionId, timeline.tasks]);

  useEffect(() => {
    if (timeline.isLoading) return;
    useTabStore.getState().patch(tab.id, {
      sessionMetaCache: {
        sessionId: timeline.sessionId,
        jsonlPath: timeline.jsonlPath,
        meta: sessionMetaFrom(timeline.sessionStats, timeline.initMeta, timeline.entries),
      },
    });
  }, [
    tab.id,
    timeline.isLoading,
    timeline.sessionId,
    timeline.jsonlPath,
    timeline.sessionStats,
    timeline.initMeta,
    timeline.entries,
  ]);

  // A finished answer on screen is seen: the sidebar's eye goes out. Retried on every status of the tab, so one that
  // arrived before the status socket opened is marked once the socket brings the tab's state.
  const surfaceVisible = useTerminalSurfaceVisible();
  const visible = useSyncExternalStore(subscribeVisibility, pageVisible, () => false);
  useEffect(() => {
    if (shouldMarkSeen(status?.cliState, surfaceVisible, visible)) dismissTab(tab.id);
  }, [tab.id, status, surfaceVisible, visible]);

  // A state the runtime could not read after its restart: it is asked to look at the screen again.
  useEffect(() => {
    if (cliState !== 'unknown') return;
    const controller = new AbortController();
    void appFetch('/api/tmux/recover-unknown', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tabId: tab.id }),
      signal: controller.signal,
    }).catch(() => {});
    return () => controller.abort();
  }, [tab.id, cliState]);

  return (
    <div className="agent-pane flex h-full min-h-0 flex-col">
      <div className="min-h-0 flex-1">
        <TimelineView
          provider={provider}
          entries={entries}
          tasks={timeline.tasks}
          sessionId={timeline.sessionId}
          sessionName={tab.sessionName}
          tabId={tab.id}
          initMeta={timeline.initMeta}
          sessionStats={timeline.sessionStats}
          cliState={cliState}
          wsStatus={timeline.wsStatus}
          isLoading={timeline.isLoading && !launch}
          error={timeline.error}
          onRetry={timeline.retrySession}
          onLoadMore={timeline.loadMore}
          hasMore={timeline.hasMore}
          jsonlPath={timeline.jsonlPath}
          liveText={timeline.liveText}
          launchingSince={status?.launchingSince ?? (launch ? launch.at : null)}
          compactingSince={status?.compactingSince ?? null}
          awaiting={cliState === 'needs-input'}
        />
      </div>
      <div className="mx-auto flex w-full max-w-content flex-col gap-2 px-4 pb-3">
        {cliState === 'inactive' && !launch && (
          <SessionEnded
            provider={provider}
            starting={timeline.resume.state === 'starting'}
            error={timeline.resume.state === 'error' ? timeline.resume.message : null}
            canResume={Boolean(timeline.sessionId)}
            onResume={() => void timeline.resumeSession()}
          />
        )}
        <PendingPrompt
          sessionName={tab.sessionName}
          provider={provider}
          waiting={cliState === 'needs-input'}
          entries={timeline.entries}
          onOpenTerminal={() => setTerminalOpen(true)}
        />
        <AgentComposer
          tabId={tab.id}
          sessionName={tab.sessionName}
          workspaceId={workspaceId}
          provider={provider}
          working={working}
          stopped={cliState === 'inactive' && !launch}
          ready={
            !status?.launchingSince &&
            (status?.cliState === 'idle' || status?.cliState === 'ready-for-review')
          }
          cwd={tab.cwd ?? null}
          onSent={(text) => {
            if (!working) addPending(text);
          }}
          onOpenTerminal={() => setTerminalOpen(true)}
        />
      </div>
      <TerminalBar
        sessionName={tab.sessionName}
        cwd={tab.cwd ?? null}
        open={terminalOpen}
        onToggle={() => setTerminalOpen((v) => !v)}
      />
    </div>
  );
};

/** A plain terminal tab (a shell, no agent) the sidebar opens: its own session, full size. */
const ShellConversation = ({ tab }: { tab: ITab }) => (
  <div className="flex h-full min-h-0 flex-col" data-testid="shell-conversation">
    <TerminalView session={tab.sessionName} focus />
  </div>
);

export default function TerminalPage() {
  useRouter();
  const selectedRef = typeof window === 'undefined' ? null : parseAppLocation(window.location.search).ref;
  const selectedTabId = selectedRef?.kind === 'agent' ? selectedRef.id : null;
  const activeWorkspaceId = useWorkspaceStore((s) => s.activeWorkspaceId);
  const layout = useLayoutStore((s) => s.layout);
  const workspaceId = useLayoutStore((s) => s.workspaceId);

  useEffect(() => {
    if (!activeWorkspaceId) return;
    useLayoutStore.getState().setWorkspaceId(activeWorkspaceId);
    void useLayoutStore.getState().fetchLayout(activeWorkspaceId);
  }, [activeWorkspaceId]);

  if (!layout || !workspaceId) return null;
  // The conversation the page shows (its address) wins over the layout's own focus.
  const panes = collectPanes(layout.root);
  const pane = panes.find((p) => p.id === layout.activePaneId) ?? panes[0];
  const addressed = selectedTabId
    ? panes.flatMap((p) => p.tabs).find((t) => t.id === selectedTabId)
    : undefined;
  const tab = addressed ?? pane?.tabs.find((t) => t.id === pane.activeTabId) ?? pane?.tabs[0];
  if (!tab) return null;
  if (tab.panelType === 'terminal' || !tab.panelType) return <ShellConversation key={tab.id} tab={tab} />;
  if (tab.panelType !== 'claude-code' && tab.panelType !== 'codex-cli') return null;
  return <AgentConversation key={tab.id} tab={tab} workspaceId={workspaceId} />;
}
