import { useMemo } from 'react';
import useAvailableFiles from '@/hooks/use-available-files';
import useConversationSources from '@/hooks/use-conversation-sources';
import useSameValue from '@/hooks/use-same-value';
import useTabMetadataStore from '@/hooks/use-tab-metadata-store';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { collectPeerMessages, collectViewableFiles } from '@/lib/hl-artifacts';
import type { ITab } from '@/types/terminal';

const EMPTY: never[] = [];

/** The timeline of a conversation with its links and work paths: what auto-open watches while the panel is hidden. */
export const usePaneSources = (mode: 'agent' | 'chat', tab: ITab | null, chatId: string | null) => {
  const sessionName = mode === 'agent' ? (tab?.sessionName ?? null) : null;
  const snapshotKey = mode === 'chat' ? (chatId ? `chat:${chatId}` : null) : sessionName;
  const snapshot = useTimelineSnapshotStore((s) => (snapshotKey ? s.bySession[snapshotKey] : undefined));
  const cwd = useTabMetadataStore((s) => (tab ? s.metadata[tab.id]?.cwd : undefined)) ?? tab?.cwd ?? null;
  const entries = snapshot?.entries ?? EMPTY;
  const tasks = snapshot?.tasks ?? EMPTY;
  const jsonlPath = snapshot?.jsonlPath ?? null;
  const { links, workPaths, complete } = useConversationSources(
    mode === 'agent' ? jsonlPath : null,
    cwd,
    entries,
  );
  const shownLinks = useSameValue(links);
  return {
    sessionName,
    jsonlPath,
    cwd,
    entries,
    tasks,
    links: shownLinks,
    workPaths,
    loaded: snapshot !== undefined,
    complete,
  };
};

/** What the right panel and its summary show of a conversation: its timeline, links, files and correspondence. */
const usePaneData = (mode: 'agent' | 'chat', tab: ITab | null, chatId: string | null) => {
  const sources = usePaneSources(mode, tab, chatId);
  const { entries } = sources;
  // Rebuilt from the whole timeline on every message; kept while their content is the same, so the tabs do not re-render.
  const peerMessages = useSameValue(useMemo(() => collectPeerMessages(entries), [entries]));
  const viewable = useSameValue(useMemo(() => collectViewableFiles(entries), [entries]));
  const files = useAvailableFiles(viewable);
  return { ...sources, peerMessages, files };
};

export type TPaneData = ReturnType<typeof usePaneData>;

export default usePaneData;
