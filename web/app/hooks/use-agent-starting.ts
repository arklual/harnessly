import { useEffect, useState } from 'react';
import useTabStore from '@/hooks/use-tab-store';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';

const SLOW_AFTER_MS = 45_000;
const FRESH_FOR_MS = 5 * 60_000;

// Only conversations opened with "New conversation" in this window get the launch screen,
// so an old empty session never pretends to be starting.
const freshTabs = new Map<string, number>();

export const markAgentLaunching = (tabId: string) => freshTabs.set(tabId, Date.now());

const ACTIVE = new Set(['busy', 'needs-input', 'ready-for-review']);

/** A new Claude is starting until its SessionStart hook arrives; after 45 s the wait is reported as slow. */
const useAgentStarting = (tabId: string | null | undefined, sessionName: string | null | undefined) => {
  const tab = useTabStore((s) => (tabId ? s.tabs[tabId] : undefined));
  const hasEntries = useTimelineSnapshotStore((s) =>
    sessionName ? (s.bySession[sessionName]?.entries.length ?? 0) > 0 : false,
  );
  const launchedAt = tabId ? freshTabs.get(tabId) : undefined;
  const [now, setNow] = useState(() => Date.now());
  const waiting =
    !!tab &&
    launchedAt !== undefined &&
    now - launchedAt < FRESH_FOR_MS &&
    !hasEntries &&
    tab.lastEvent?.name !== 'session-start' &&
    !ACTIVE.has(tab.cliState) &&
    tab.agentProcess !== false;

  useEffect(() => {
    if (!waiting || launchedAt === undefined) return;
    const timer = setTimeout(
      () => setNow(Date.now()),
      Math.max(0, launchedAt + SLOW_AFTER_MS - Date.now()) + 50,
    );
    return () => clearTimeout(timer);
  }, [waiting, launchedAt]);

  useEffect(() => {
    if (tabId && launchedAt !== undefined && !waiting) freshTabs.delete(tabId);
  }, [tabId, launchedAt, waiting]);

  const slow = waiting && launchedAt !== undefined && now - launchedAt >= SLOW_AFTER_MS;
  return { starting: waiting && !slow, slow };
};

export default useAgentStarting;
