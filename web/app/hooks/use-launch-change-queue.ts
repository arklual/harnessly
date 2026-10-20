import { useEffect } from 'react';
import { create } from 'zustand';
import { toast } from 'sonner';
import useTabStore from '@/hooks/use-tab-store';
import { tabIdOfSession } from '@/hooks/use-background-tasks';
import { appFetch } from '@/lib/app-path';
import {
  CONFIRMED_TTL_MS,
  dueChanges,
  nextApplying,
  expireConfirmed,
  nextRetryAt,
  observeBusy,
  pruneConfirmed,
  releaseQueued,
  requestChange,
  requeueChange,
  settleChange,
  type ILaunchChange,
  type TLaunchKind,
  type TLaunchQueue,
} from '@/lib/launch-change-queue';
import type { TCliState } from '@/types/timeline';
import { t } from '@/lib/i18n';

interface ILaunchChangeStore {
  queue: TLaunchQueue;
  request: (
    session: string,
    change: { kind: TLaunchKind; value: string; from: string | null },
    busy: boolean,
  ) => void;
  settle: (session: string, kind: TLaunchKind, ok: boolean, value: string) => void;
  requeue: (session: string, kind: TLaunchKind, value: string) => void;
  release: (session: string) => void;
  observeBusy: (session: string) => void;
  expire: () => void;
  prune: (session: string, kind: TLaunchKind, reported: string | null) => void;
}

type TQueueState = Pick<ILaunchChangeStore, 'queue'>;

/** A no-op change returns the same state, so subscribers are not woken (the drain would otherwise loop on itself). */
const update = (state: TQueueState, queue: TLaunchQueue): TQueueState =>
  queue === state.queue ? state : { queue };

export const useLaunchChangeStore = create<ILaunchChangeStore>((set) => ({
  queue: {},
  request: (session, change, busy) =>
    set((state) => update(state, requestChange(state.queue, session, change, busy))),
  settle: (session, kind, ok, value) =>
    set((state) => update(state, settleChange(state.queue, session, kind, ok, value, Date.now()))),
  requeue: (session, kind, value) =>
    set((state) => update(state, requeueChange(state.queue, session, kind, value, Date.now()))),
  release: (session) => set((state) => update(state, releaseQueued(state.queue, session, Date.now()))),
  observeBusy: (session) => set((state) => update(state, observeBusy(state.queue, session))),
  expire: () => set((state) => update(state, expireConfirmed(state.queue, Date.now()))),
  prune: (session, kind, reported) =>
    set((state) => update(state, pruneConfirmed(state.queue, session, kind, reported))),
}));

/** The agent takes a switch only between turns: a running turn or an open question leaves it for later. */
export const isAgentBusy = (cliState: TCliState | undefined) =>
  cliState === 'busy' || cliState === 'needs-input';

export const isSessionBusy = (sessionName: string) => {
  const tabId = tabIdOfSession(sessionName);
  return isAgentBusy(tabId ? useTabStore.getState().tabs[tabId]?.cliState : undefined);
};

type TApplyResult = 'applied' | 'busy' | 'failed';

interface ILaunchResponse {
  error?: string;
  code?: string;
  message?: string;
  outcome?: string;
}

/** A success is silent: the chip and the line in the timeline already show it; a failure shows the server's reason. */
const applyLaunchChange = async (
  sessionName: string,
  change: Pick<ILaunchChange, 'kind' | 'value'>,
): Promise<TApplyResult> => {
  const request =
    change.kind === 'model'
      ? { url: '/api/platform/switch-model', body: { session: sessionName, model: change.value } }
      : {
          url: '/api/platform/commands/run',
          body: { session: sessionName, provider: 'claude', line: `/effort ${change.value}` },
        };
  const response = await appFetch(request.url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request.body),
  }).catch(() => null);
  const body = (await response?.json().catch(() => null)) as ILaunchResponse | null;
  if (response?.ok && (change.kind === 'model' || body?.outcome === 'sent' || body?.outcome === 'confirmed'))
    return 'applied';
  if (response?.status === 409 && (body?.code === 'busy' || body?.error === 'busy')) return 'busy';
  if (response?.ok) toast.error(t('hooks.useLaunchChangeQueue.effortRejected'));
  else
    toast.error(
      body?.message ??
        body?.error ??
        (response
          ? t('hooks.useLaunchChangeQueue.rejected')
          : t('hooks.useLaunchChangeQueue.machineNoAnswer')),
    );
  return 'failed';
};

const inFlight = new Set<string>();

const wakeAt = (at: number, run: () => void) => setTimeout(run, Math.max(0, at - Date.now()) + 1);

/** One request per session at a time, the model before the effort; a pick made meanwhile goes out when the previous one settles. */
const pump = (session: string) => {
  if (inFlight.has(session)) return;
  const change = nextApplying(useLaunchChangeStore.getState().queue, session);
  if (!change) return;
  inFlight.add(session);
  void send(session, change);
};

const send = async (session: string, change: ILaunchChange) => {
  let result: TApplyResult = 'failed';
  try {
    result = await applyLaunchChange(session, change);
  } catch {
    toast.error(t('hooks.useLaunchChangeQueue.failed'));
  } finally {
    const store = useLaunchChangeStore.getState();
    if (result === 'busy') store.requeue(session, change.kind, change.value);
    else store.settle(session, change.kind, result === 'applied', change.value);
    inFlight.delete(session);
    if (result === 'applied') wakeAt(Date.now() + CONFIRMED_TTL_MS, store.expire);
    const retryAt = result === 'busy' ? nextRetryAt(useLaunchChangeStore.getState().queue, session) : null;
    if (retryAt !== null) wakeAt(retryAt, drainLaunchChanges);
    pump(session);
  }
};

export const requestLaunchChange = (
  sessionName: string,
  change: { kind: TLaunchKind; value: string; from: string | null },
) => {
  const busy = isSessionBusy(sessionName);
  useLaunchChangeStore.getState().request(sessionName, change, busy);
  if (!busy) pump(sessionName);
};

export const drainLaunchChanges = () => {
  const { queue, release, observeBusy: seeBusy } = useLaunchChangeStore.getState();
  for (const session of Object.keys(queue)) {
    const busy = isSessionBusy(session);
    if (busy) seeBusy(session);
    if (!dueChanges(useLaunchChangeStore.getState().queue, session, busy, Date.now()).length) continue;
    release(session);
    pump(session);
  }
};

/** Hands queued switches to the agent once its tab leaves busy / needs-input; alive as long as the status socket. */
export const useLaunchChangeDrain = () => {
  useEffect(() => {
    const offTabs = useTabStore.subscribe(drainLaunchChanges);
    const offQueue = useLaunchChangeStore.subscribe(drainLaunchChanges);
    return () => {
      offTabs();
      offQueue();
    };
  }, []);
};
