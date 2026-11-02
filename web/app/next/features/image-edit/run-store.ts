// The changes being made, kept outside the viewer: closing it does not stop a run (a real one takes a minute or two),
// reopening shows it again. Keyed by the original version's id, one run at a time for a picture. A run is started
// through the host, followed by asking it every RUN_POLL_MS, and stays in the store (done or failed) until the viewer
// has taken it in or the person dismisses it.
import { useSyncExternalStore } from 'react';
import type {
  IEditProgress,
  IImageEdit,
  IImageEditHost,
  IImageVersion,
} from '@/next/features/image-edit/host';

export const RUN_STATE = { running: 'running', done: 'done', failed: 'failed' } as const;
export type TRunState = (typeof RUN_STATE)[keyof typeof RUN_STATE];

export const RUN_POLL_MS = 2000;
/** The host not answering this many times in a row ends the run as failed (a single hiccup does not). */
export const RUN_MAX_MISSES = 5;

export interface IRun {
  state: TRunState;
  edit: IImageEdit;
  /** The new versions, when done. */
  versions: readonly IImageVersion[];
  error: string | null;
}

interface IEntry {
  run: IRun;
  host: IImageEditHost;
  abort: AbortController;
}

const entries = new Map<string, IEntry>();
const listeners = new Set<() => void>();
const notify = () => listeners.forEach((listener) => listener());

const reasonOf = (error: unknown): string => (error instanceof Error ? error.message : String(error));

const put = (key: string, entry: IEntry) => {
  entries.set(key, entry);
  notify();
};

const finish = (key: string, entry: IEntry, progress: IEditProgress) => {
  if (entries.get(key) !== entry) return;
  const state = progress.state === 'done' ? RUN_STATE.done : RUN_STATE.failed;
  put(key, {
    ...entry,
    run: { ...entry.run, state, versions: progress.versions, error: progress.error ?? null },
  });
  // The new version is chosen here, not by the viewer: the window may have been closed long ago.
  const last = progress.versions[progress.versions.length - 1];
  if (state === RUN_STATE.done && last !== undefined) entry.host.chosen?.(last);
};

const wait = (ms: number, signal: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });

const follow = async (key: string, entry: IEntry, runId: string) => {
  let misses = 0;
  while (entries.get(key) === entry) {
    await wait(RUN_POLL_MS, entry.abort.signal);
    if (entry.abort.signal.aborted) return;
    try {
      const progress = await entry.host.read(runId, entry.abort.signal);
      misses = 0;
      if (progress.state !== RUN_STATE.running) {
        finish(key, entry, progress);
        return;
      }
    } catch (error) {
      if (entry.abort.signal.aborted) return;
      misses += 1;
      if (misses >= RUN_MAX_MISSES) {
        finish(key, entry, { state: RUN_STATE.failed, versions: [], error: reasonOf(error) });
        return;
      }
    }
  }
};

/** Starts a change of the picture `key` (the original's id). A refusal (too many jobs) ends it as failed with its reason. */
export const startRun = (key: string, host: IImageEditHost, edit: IImageEdit): void => {
  entries.get(key)?.abort.abort();
  const entry: IEntry = {
    run: { state: RUN_STATE.running, edit, versions: [], error: null },
    host,
    abort: new AbortController(),
  };
  put(key, entry);
  host
    .start(edit)
    .then((runId) => follow(key, entry, runId))
    .catch((error: unknown) => {
      if (entries.get(key) !== entry) return;
      finish(key, entry, { state: RUN_STATE.failed, versions: [], error: reasonOf(error) });
    });
};

/** The same change again, after it failed. */
export const retryRun = (key: string): void => {
  const entry = entries.get(key);
  if (entry && entry.run.state === RUN_STATE.failed) startRun(key, entry.host, entry.run.edit);
};

/** Forgets the run (its result has been taken in, or the person moved on). */
export const dismissRun = (key: string): void => {
  const entry = entries.get(key);
  if (!entry) return;
  entry.abort.abort();
  entries.delete(key);
  notify();
};

export const runOf = (key: string): IRun | null => entries.get(key)?.run ?? null;

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

export const useRun = (key: string): IRun | null =>
  useSyncExternalStore(
    subscribe,
    () => runOf(key),
    () => null,
  );

/** For tests: drops every run. */
export const resetRuns = (): void => {
  entries.forEach((entry) => entry.abort.abort());
  entries.clear();
  notify();
};
