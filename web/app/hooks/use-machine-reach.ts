import { useEffect, useState } from 'react';
import { scopedAppPath } from '@/lib/app-path';
import {
  isMachineDown,
  nextReach,
  REACH_POLL_MS,
  REACH_RECHECK_MS,
  REACH_UNKNOWN,
  type IReachAnswer,
  type IReachState,
} from '@/lib/machine-reach';

const ask = async (machineId: string, signal: AbortSignal): Promise<IReachAnswer | null> => {
  try {
    const res = await fetch(scopedAppPath('/api/platform/machine-reach', machineId), {
      signal,
      cache: 'no-store',
    });
    if (!res.ok) return null;
    const body = (await res.json()) as Partial<IReachAnswer>;
    return typeof body.reachable === 'boolean'
      ? { reachable: body.reachable, since: typeof body.since === 'number' ? body.since : null }
      : null;
  } catch {
    return null;
  }
};

/**
 * Whether the machine of the conversation on screen answers, asked of the gateway every few seconds while
 * the page is visible; `listedAvailable` is what the stored list says from the machine's heartbeats.
 */
const useMachineReach = (
  machineId: string | null,
  listedAvailable: boolean | undefined,
): { down: boolean; up: boolean; since: number | null } => {
  const [state, setState] = useState<{ machineId: string | null; reach: IReachState }>({
    machineId,
    reach: REACH_UNKNOWN,
  });
  if (state.machineId !== machineId) setState({ machineId, reach: REACH_UNKNOWN });

  useEffect(() => {
    if (!machineId) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | null = null;
    const poll = async () => {
      let missed = false;
      if (document.visibilityState === 'visible') {
        const answer = await ask(machineId, controller.signal);
        if (controller.signal.aborted) return;
        missed = answer?.reachable === false;
        setState((current) =>
          current.machineId === machineId
            ? { machineId, reach: nextReach(current.reach, answer, Date.now()) }
            : current,
        );
      }
      if (!controller.signal.aborted) timer = setTimeout(poll, missed ? REACH_RECHECK_MS : REACH_POLL_MS);
    };
    void poll();
    const wake = () => {
      if (document.visibilityState === 'visible' && timer) {
        clearTimeout(timer);
        timer = null;
        void poll();
      }
    };
    document.addEventListener('visibilitychange', wake);
    return () => {
      controller.abort();
      if (timer) clearTimeout(timer);
      document.removeEventListener('visibilitychange', wake);
    };
  }, [machineId]);

  const reach = state.machineId === machineId ? state.reach : REACH_UNKNOWN;
  const down = Boolean(machineId) && isMachineDown(reach, listedAvailable);
  return { down, up: reach.answered, since: down ? reach.since : null };
};

export default useMachineReach;
