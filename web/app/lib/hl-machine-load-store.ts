import { t } from '@/lib/i18n';
import { useCallback, useEffect, useSyncExternalStore } from 'react';
import { toast } from 'sonner';
import { appFetch, currentMachineId } from '@/lib/app-path';
import { createPoller, type IPoller } from '@/lib/poller';
import type { IMachineLoad, IMachineProcess } from '@/types/server/hl-machine-load-scan';
import {
  LOAD_STALE_MS,
  enteredOverload,
  isLoadStale,
  machineKey,
  machineLoadPath,
  metricText,
  processKey,
  pruneStopped,
  stopOutcome,
  type TStopOutcome,
} from '@/lib/hl-machine-load-view';

/**
 * One poller per machine for the whole page: the header indicator, its popover, the "Window" island and the
 * preview gates read the same snapshot. It polls every 5 s while anything subscribes and the tab is visible.
 */

const POLL = { intervalMs: 5_000, maxBackoffMs: 30_000, staleMs: 5_000 } as const;
const NOTICE_MS = 6_000;
/** How long a stopped process may still be listed (SIGKILL comes after 5 s) before its row offers "Stop" again. */
const STOPPED_MS = 10_000;

export interface IMachineLoadNotice {
  tone: 'info' | 'error';
  text: string;
}

export interface IMachineLoadSnapshot {
  /** null until the first answer since the machine got its first subscriber. */
  load: IMachineLoad | null;
  error: string | null;
  /** The last poll failed or the load is older than `LOAD_STALE_MS`: shown muted, never acted on. */
  stale: boolean;
  /** `pid:starttime` of the processes a stop request is in flight for. */
  stopping: ReadonlySet<string>;
  /** `pid:starttime` → until when a stopped process is shown as "Stopped". */
  stopped: ReadonlyMap<string, number>;
  notice: IMachineLoadNotice | null;
}

const EMPTY: IMachineLoadSnapshot = {
  load: null,
  error: null,
  stale: false,
  stopping: new Set(),
  stopped: new Map(),
  notice: null,
};

interface IEntry {
  snapshot: IMachineLoadSnapshot;
  poller: IPoller;
  subscribers: number;
  /** Subscribers that speak for the page: only they toast, so a runtime frame never repeats the shell's toast. */
  notifiers: number;
  noticeTimer: ReturnType<typeof setTimeout> | null;
  staleTimer: ReturnType<typeof setTimeout> | null;
  receivedAt: number | null;
  /** Survives the skeleton reset, so coming back to a machine that is still overloaded does not toast again. */
  lastLevel: IMachineLoad['level'] | null;
}

const entries = new Map<string, IEntry>();
const listeners = new Set<() => void>();

const emit = () => {
  for (const listener of listeners) listener();
};

const keyOf = (machine: string) => machineKey(machine, currentMachineId());

const failure = (error: unknown) =>
  error instanceof Error ? error.message : t('lib.hlMachineLoadStore.noConnection');

const patch = (key: string, next: Partial<IMachineLoadSnapshot>) => {
  const entry = entries.get(key);
  if (!entry) return;
  entry.snapshot = { ...entry.snapshot, ...next };
  emit();
};

const fetchLoad = async (key: string, signal: AbortSignal): Promise<IMachineLoad> => {
  const response = await appFetch(machineLoadPath('/api/platform/machine/load', key), {
    signal,
    cache: 'no-store',
  });
  if (!response.ok) throw new Error(t('lib.hlMachineLoadStore.loadUnavailable', { status: response.status }));
  return (await response.json()) as IMachineLoad;
};

const markStaleLater = (key: string, entry: IEntry) => {
  if (entry.staleTimer) clearTimeout(entry.staleTimer);
  entry.staleTimer = setTimeout(() => {
    entry.staleTimer = null;
    if (isLoadStale(entry.receivedAt, false, Date.now())) patch(key, { stale: true });
  }, LOAD_STALE_MS + 50);
};

const entryOf = (key: string): IEntry => {
  const existing = entries.get(key);
  if (existing) return existing;
  const entry: IEntry = {
    snapshot: EMPTY,
    subscribers: 0,
    notifiers: 0,
    noticeTimer: null,
    staleTimer: null,
    receivedAt: null,
    lastLevel: null,
    poller: createPoller({
      ...POLL,
      run: async (signal) => {
        let load: IMachineLoad;
        try {
          load = await fetchLoad(key, signal);
        } catch (error) {
          if (!signal.aborted) patch(key, { error: failure(error), stale: true });
          throw error;
        }
        const previous = entry.lastLevel;
        entry.lastLevel = load.level;
        if (entry.notifiers > 0 && enteredOverload(previous, load.level)) {
          toast.warning(
            t('lib.hlMachineLoadStore.overloaded', { metric: metricText(load.reason ?? 'ram', load) }),
            {
              description: t('lib.hlMachineLoadStore.overloadedHint'),
            },
          );
        }
        entry.receivedAt = Date.now();
        markStaleLater(key, entry);
        patch(key, {
          load,
          error: null,
          stale: false,
          stopped: pruneStopped(entry.snapshot.stopped, load.top, entry.receivedAt),
        });
      },
    }),
  };
  entries.set(key, entry);
  return entry;
};

const subscribeMachine = (key: string, notify: boolean) => {
  const entry = entryOf(key);
  entry.subscribers += 1;
  if (notify) entry.notifiers += 1;
  if (entry.subscribers === 1) {
    // A machine coming back shows a skeleton, not the level it had minutes ago.
    entry.receivedAt = null;
    entry.snapshot = { ...entry.snapshot, load: null, error: null, stale: false };
    emit();
    entry.poller.start();
  }
  return () => {
    entry.subscribers -= 1;
    if (notify) entry.notifiers -= 1;
    if (entry.subscribers > 0) return;
    entry.poller.stop();
    if (entry.staleTimer) clearTimeout(entry.staleTimer);
    entry.staleTimer = null;
  };
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const readMachineLoad = (machine: string): IMachineLoadSnapshot =>
  entries.get(keyOf(machine))?.snapshot ?? EMPTY;

const showNotice = (key: string, notice: IMachineLoadNotice | null) => {
  const entry = entries.get(key);
  if (!entry) return;
  if (entry.noticeTimer) clearTimeout(entry.noticeTimer);
  entry.noticeTimer = notice ? setTimeout(() => showNotice(key, null), NOTICE_MS) : null;
  patch(key, { notice });
};

const withStopping = (key: string, proc: string, on: boolean) => {
  const next = new Set(entryOf(key).snapshot.stopping);
  if (on) next.add(proc);
  else next.delete(proc);
  patch(key, { stopping: next });
};

const markStopped = (key: string, proc: string) => {
  const until = Date.now() + STOPPED_MS;
  patch(key, { stopped: new Map(entryOf(key).snapshot.stopped).set(proc, until) });
  setTimeout(() => {
    const entry = entries.get(key);
    if (entry?.snapshot.stopped.get(proc) === until)
      patch(key, {
        stopped: pruneStopped(entry.snapshot.stopped, entry.snapshot.load?.top ?? [], Date.now()),
      });
  }, STOPPED_MS + 50);
};

/** Pessimistic: the row says "Stopping…" until the runtime answers, then "Stopped" until the process is gone. */
export const stopMachineProcess = async (
  machine: string,
  proc: Pick<IMachineProcess, 'pid' | 'starttime'>,
): Promise<TStopOutcome> => {
  const key = keyOf(machine);
  const entry = entryOf(key);
  const procKey = processKey(proc);
  withStopping(key, procKey, true);
  showNotice(key, null);
  let outcome: TStopOutcome;
  try {
    const response = await appFetch(machineLoadPath('/api/platform/machine/stop', key), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pid: proc.pid, starttime: proc.starttime }),
    });
    outcome = stopOutcome(response.status, await response.json().catch(() => null));
  } catch (error) {
    outcome = { kind: 'error', message: failure(error) };
  }
  if (outcome.kind === 'stopped') markStopped(key, procKey);
  if (outcome.kind !== 'error') await entry.poller.refresh();
  withStopping(key, procKey, false);
  if (outcome.kind === 'refresh') showNotice(key, { tone: 'info', text: outcome.message });
  if (outcome.kind === 'error') showNotice(key, { tone: 'error', text: outcome.message });
  return outcome;
};

interface IUseMachineLoadOptions {
  /** Polls only while true. */
  enabled?: boolean;
  /** Toast on entering overload (the shell header, not every reader). */
  notify?: boolean;
}

export const useMachineLoad = (
  machine: string,
  { enabled = true, notify = false }: IUseMachineLoadOptions = {},
) => {
  const key = keyOf(machine);
  useEffect(() => (enabled ? subscribeMachine(key, notify) : undefined), [key, enabled, notify]);
  const snapshot = useSyncExternalStore(
    subscribe,
    () => entries.get(key)?.snapshot ?? EMPTY,
    () => EMPTY,
  );
  const stop = useCallback(
    (proc: Pick<IMachineProcess, 'pid' | 'starttime'>) => stopMachineProcess(key, proc),
    [key],
  );
  return { ...snapshot, key, stop };
};
