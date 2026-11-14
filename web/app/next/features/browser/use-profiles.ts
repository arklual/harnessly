import { useEffect, useState } from 'react';
import { loadProfiles, type IProfilesPage } from './profiles-api';
import { calmPoll } from '@/next/core/net/calm-poll';
import type { MachineId } from '@/next/protocol/v2';

/** The step while the profiles change, and the longest one while they stay the same. */
const PROFILES_POLL_MS = 3000;
const PROFILES_QUIET_MS = 15_000;

/** Reads only the machine on screen; hidden pages leave requests waiting on the runtime. */
export function useProfiles(machine: MachineId | null, poll = false) {
  const [read, setRead] = useState<{ machine: MachineId; data: IProfilesPage } | null>(null);
  const [failure, setFailure] = useState<{ machine: MachineId; text: string } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!machine) return;
    let gone = false,
      busy = false;
    let last: string | null = null;
    /** One read; true when the profiles changed. */
    const look = async (): Promise<boolean> => {
      if (busy) return false;
      busy = true;
      try {
        const data = await loadProfiles(machine);
        if (gone) return false;
        const said = JSON.stringify(data);
        const moved = said !== last;
        last = said;
        if (moved) setRead({ machine, data });
        setFailure(null);
        return moved;
      } catch (error) {
        if (!gone) setFailure({ machine, text: String(error instanceof Error ? error.message : error) });
        return false;
      } finally {
        busy = false;
      }
    };
    const changed = (event: Event) => {
      if ((event as CustomEvent).detail === machine) setAttempt((n) => n + 1);
    };
    window.addEventListener('browser-profiles-changed', changed);
    // Polled while the page is visible, slower while nothing changes; read once otherwise.
    const stop = poll ? calmPoll({ base: PROFILES_POLL_MS, max: PROFILES_QUIET_MS, look }) : null;
    if (!poll) void look();
    const onVisible = () => {
      if (!poll && document.visibilityState === 'visible') void look();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      gone = true;
      stop?.();
      window.removeEventListener('browser-profiles-changed', changed);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [machine, poll, attempt]);
  return {
    data: read?.machine === machine ? read.data : null,
    error: failure?.machine === machine ? failure.text : null,
    retry: () => setAttempt((n) => n + 1),
  };
}
