import { useEffect, useState } from 'react';
import { loadProfiles, type IProfilesPage } from './profiles-api';
import type { MachineId } from '@/next/protocol/v2';

/** Reads only the machine on screen; hidden pages leave requests waiting on the runtime. */
export function useProfiles(machine: MachineId | null, poll = false) {
  const [read, setRead] = useState<{ machine: MachineId; data: IProfilesPage } | null>(null);
  const [failure, setFailure] = useState<{ machine: MachineId; text: string } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!machine) return;
    let gone = false,
      busy = false;
    const look = async () => {
      if (busy || (poll && document.visibilityState !== 'visible')) return;
      busy = true;
      try {
        const data = await loadProfiles(machine);
        if (!gone) {
          setRead({ machine, data });
          setFailure(null);
        }
      } catch (error) {
        if (!gone) setFailure({ machine, text: String(error instanceof Error ? error.message : error) });
      } finally {
        busy = false;
      }
    };
    const changed = (event: Event) => {
      if ((event as CustomEvent).detail === machine) setAttempt((n) => n + 1);
    };
    void look();
    const timer = poll ? setInterval(() => void look(), 3000) : undefined;
    window.addEventListener('browser-profiles-changed', changed);
    document.addEventListener('visibilitychange', look);
    return () => {
      gone = true;
      clearInterval(timer);
      window.removeEventListener('browser-profiles-changed', changed);
      document.removeEventListener('visibilitychange', look);
    };
  }, [machine, poll, attempt]);
  return {
    data: read?.machine === machine ? read.data : null,
    error: failure?.machine === machine ? failure.text : null,
    retry: () => setAttempt((n) => n + 1),
  };
}
