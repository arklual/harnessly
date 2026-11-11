// What the pane's contents read of a conversation, kept in Legacy's snapshot store while the pane is out. A runtime that
// gathers it sends it in the conversation's own channel (stores/conversation, `pane`): nothing is opened here. Only a
// runtime that sends none is read the old way, by Legacy's timeline socket. Part of the bridge: it goes with it.
import { useEffect, useMemo } from 'react';
import useTimeline from '@/hooks/use-timeline';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import type { ITab } from '@/types/terminal';
import { paneEntries, paneLinks } from '@/next/legacy/pane-feed';
import { usePane, type PaneView } from '@/next/stores/conversation';
import type { Conversation } from '@/next/protocol/v2';

const NO_TASKS: never[] = [];

/**
 * Keeps what the runtime gathered of the conversation for its pane in the snapshot store while mounted: the contents
 * read it there, as they read a timeline. It came by the conversation's own channel: no socket is opened here.
 */
const Fed = ({ tab, pane }: { tab: ITab; pane: Extract<PaneView, { fed: true }> }) => {
  const entries = useMemo(() => paneEntries(pane.items), [pane.items]);
  const links = useMemo(() => paneLinks(pane.links), [pane.links]);
  const jsonlPath = tab.agentState?.jsonlPath ?? null;
  const sessionId = tab.agentState?.sessionId ?? null;
  useEffect(() => {
    useTimelineSnapshotStore.getState().publish(tab.sessionName, {
      entries,
      jsonlPath,
      sessionId,
      tasks: NO_TASKS,
      links,
      activity: pane.count,
    });
  }, [tab.sessionName, entries, links, jsonlPath, sessionId, pane.count]);
  return null;
};

/** Keeps the Legacy timeline of the conversation in the snapshot store while mounted: the contents read it there. */
const Timeline = ({ tab, provider, machineId }: { tab: ITab; provider: string; machineId: string }) => {
  const timeline = useTimeline({
    sessionName: tab.sessionName,
    agentSessionId: tab.agentState?.sessionId ?? null,
    panelType: provider === 'codex' ? 'codex-cli' : 'claude-code',
    machineId,
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

/**
 * What the pane's contents read of the conversation: from the conversation's channel when its runtime sends it, and
 * only from a runtime that sends none over the Legacy timeline socket. Nothing until the conversation's snapshot says
 * which.
 */
export const PaneSource = ({ tab, conv }: { tab: ITab; conv: Conversation }) => {
  const pane = usePane(conv.id);
  if (!pane) return null;
  if (pane.fed) return <Fed tab={tab} pane={pane} />;
  return <Timeline tab={tab} provider={conv.provider} machineId={conv.machineId} />;
};
