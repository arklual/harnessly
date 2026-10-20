import { appFetch } from '@/lib/app-path';
import { t } from '@/lib/i18n';
import type { TCheckpointView } from '@/lib/hl-checkpoint-match';
import type { ICheckpointDiff, IRewindRecord, ISkippedFile } from '@/types/server/hl-checkpoints';

export interface ICheckpointsState {
  checkpoints: TCheckpointView[];
  rewinds: IRewindRecord[];
  busy: boolean;
}

export interface IRewindDone {
  before: TCheckpointView;
  changed: string[];
  skipped: ISkippedFile[];
}

const EMPTY: ICheckpointsState = { checkpoints: [], rewinds: [], busy: false };
/** A turn's snapshot is taken after its stop is reported and git takes its time on big trees: ask again a few times. */
const RETRY_DELAYS_MS = [1500, 4000, 10_000];
const MIN_GAP_MS = 800;

const states = new Map<string, ICheckpointsState>();
const inflight = new Map<string, Promise<void>>();
const lastLoad = new Map<string, number>();
const retries = new Map<string, ReturnType<typeof setTimeout>[]>();
const listeners = new Set<() => void>();

const emit = () => listeners.forEach((listener) => listener());

export const subscribeCheckpoints = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const getCheckpointsState = (tabId: string): ICheckpointsState => states.get(tabId) ?? EMPTY;

const sameState = (a: ICheckpointsState, b: ICheckpointsState) => JSON.stringify(a) === JSON.stringify(b);

export const refreshCheckpoints = (tabId: string): Promise<void> => {
  const running = inflight.get(tabId);
  if (running) return running;
  if (Date.now() - (lastLoad.get(tabId) ?? 0) < MIN_GAP_MS) return Promise.resolve();
  const load = appFetch(`/api/platform/checkpoints?${new URLSearchParams({ tabId })}`)
    .then((response) => (response.ok ? (response.json() as Promise<ICheckpointsState>) : null))
    .then((next) => {
      if (next && !sameState(getCheckpointsState(tabId), next)) {
        states.set(tabId, next);
        emit();
      }
    })
    .catch(() => {})
    .finally(() => {
      lastLoad.set(tabId, Date.now());
      inflight.delete(tabId);
    });
  inflight.set(tabId, load);
  return load;
};

/** After a turn settles: reload now-ish and again later, until the caller stops asking because the snapshot has arrived. */
export const watchForCheckpoint = (tabId: string) => {
  (retries.get(tabId) ?? []).forEach(clearTimeout);
  retries.set(
    tabId,
    RETRY_DELAYS_MS.map((delay) => setTimeout(() => void refreshCheckpoints(tabId), delay)),
  );
};

export const stopWatchingCheckpoint = (tabId: string) => {
  (retries.get(tabId) ?? []).forEach(clearTimeout);
  retries.delete(tabId);
};

const errorOf = async (response: Response) => {
  const body = (await response.json().catch(() => null)) as { error?: string } | null;
  return body?.error ?? t('lib.checkpointClient.errorStatus', { status: response.status });
};

export const loadCheckpointDiff = async (
  tabId: string,
  id: string,
): Promise<{ ok: true; diff: ICheckpointDiff } | { ok: false; error: string }> => {
  try {
    const response = await appFetch(`/api/platform/checkpoints?${new URLSearchParams({ tabId, id })}`);
    return response.ok
      ? { ok: true, diff: (await response.json()) as ICheckpointDiff }
      : { ok: false, error: await errorOf(response) };
  } catch {
    return { ok: false, error: t('lib.checkpointClient.noConnection') };
  }
};

export const rewindTo = async (
  tabId: string,
  id: string,
): Promise<{ ok: true; done: IRewindDone } | { ok: false; error: string }> => {
  try {
    const response = await appFetch('/api/platform/checkpoints', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tabId, id }),
    });
    if (!response.ok) return { ok: false, error: await errorOf(response) };
    const done = (await response.json()) as IRewindDone;
    lastLoad.delete(tabId);
    await refreshCheckpoints(tabId);
    return { ok: true, done };
  } catch {
    return { ok: false, error: t('lib.checkpointClient.noConnection') };
  }
};
