import { useContext, useEffect, useRef, useState } from 'react';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import useTabStore from '@/hooks/use-tab-store';
import {
  PENDING_LAUNCH_MAX_AGE_MS,
  UNASSIGNED_MAX_AGE_MS,
  UNASSIGNED_TAB,
  clearPendingLaunch,
  onPendingLaunchChange,
  readPendingLaunch,
  type IPendingLaunch,
} from '@/lib/hl-pending-launch';
import type { IPendingMessageOptions, ITimelineEntry } from '@/types/timeline';

const hasRealUserMessage = (entries: readonly ITimelineEntry[]) =>
  entries.some((entry) => entry.type === 'user-message' && !entry.pending);

const readLaunch = (machineId: string | undefined, tabId: string, fresh: boolean): IPendingLaunch | null => {
  const own = readPendingLaunch(machineId, tabId);
  if (own || !fresh) return own;
  const unassigned = readPendingLaunch(machineId, UNASSIGNED_TAB);
  return unassigned && Date.now() - unassigned.at <= UNASSIGNED_MAX_AGE_MS ? unassigned : null;
};

/**
 * The prompt to seed the timeline with, read once on mount: the first frame of the panel already carries
 * the message, instead of a skeleton until an effect adds it.
 */
export const useLaunchSeed = (tabId: string): string | null => {
  const machineId = useContext(RuntimeMachineContext);
  const fresh = useTabStore((s) => !s.tabs[tabId]?.agentSessionId);
  const [seed] = useState(() => readLaunch(machineId, tabId, fresh)?.prompt || null);
  return seed;
};

/**
 * The launch of a conversation the shell just created: its first message goes into the timeline as a
 * pending one at once, so the start reads as «message, then Claude starting under it». The launch ends when Claude
 * takes the message (or, without one, reports its first hook event), when the transcript records a real message,
 * or when it expires; an ended launch does not come back for that tab.
 */
const usePendingLaunch = (
  tabId: string,
  entries: readonly ITimelineEntry[],
  addPendingUserMessage: (text: string, options?: IPendingMessageOptions) => string,
): IPendingLaunch | null => {
  const machineId = useContext(RuntimeMachineContext);
  const fresh = useTabStore((s) => !s.tabs[tabId]?.agentSessionId);
  const read = () => readLaunch(machineId, tabId, fresh);
  const [state, setState] = useState(() => ({ tabId, launch: read() }));
  const shown = useRef<string | null>(null);
  const launch = state.tabId === tabId ? state.launch : null;

  const agentUp = useTabStore((s) => {
    const tab = s.tabs[tabId];
    if (!tab?.lastEvent) return false;
    return (
      !launch?.prompt ||
      tab.cliState === 'busy' ||
      tab.cliState === 'needs-input' ||
      tab.cliState === 'ready-for-review'
    );
  });
  const done = !!launch && (agentUp || hasRealUserMessage(entries));

  if (state.tabId !== tabId) setState({ tabId, launch: read() });
  else if (done) setState({ tabId, launch: null });

  useEffect(() => {
    if (!launch || !launch.prompt || shown.current === tabId) return;
    shown.current = tabId;
    if (!entries.some((entry) => entry.type === 'user-message'))
      addPendingUserMessage(launch.prompt, { autoHide: false, launch: true });
  }, [launch, tabId, entries, addPendingUserMessage]);

  useEffect(
    () =>
      onPendingLaunchChange(() =>
        setState((current) =>
          current.tabId === tabId && current.launch ? current : { tabId, launch: read() },
        ),
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `read` closes over these
    [machineId, tabId, fresh],
  );

  useEffect(() => {
    if (done) clearPendingLaunch(machineId, tabId);
  }, [done, machineId, tabId]);

  useEffect(() => {
    if (!launch) return;
    const timer = setTimeout(
      () => setState({ tabId, launch: read() }),
      Math.max(0, launch.at + PENDING_LAUNCH_MAX_AGE_MS - Date.now()) + 50,
    );
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `read` closes over these
  }, [launch, machineId, tabId, fresh]);

  return done ? null : launch;
};

export default usePendingLaunch;
