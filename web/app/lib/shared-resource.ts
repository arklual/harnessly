import { createPoller, type IVisibilitySource, type TPollCause } from '@/lib/poller';

/**
 * One polled value shared by every component that shows it: a single request stream no matter how many
 * subscribers mount, and the last good value survives unmounts so a reopened panel renders at once.
 * Plug into React with `useSharedResource` (`@/hooks/use-shared-resource`): a poll renders as deferred work there.
 */

export interface IResourceState<T> {
  /** `null` until the first successful load. */
  readonly data: T | null;
  /** Message for the last failed load; cleared by the next success. Earlier data stays on screen meanwhile. */
  readonly error: string | null;
  /** Set by `update`: a change made on this page, which its screen shows in the same frame, not after a poll's pace. */
  readonly local?: true;
}

export interface ISharedResourceOptions<T> {
  /** `cause` is `signal` when the load follows a known change: a server-side cache must not answer it. */
  load: (signal: AbortSignal, cause: TPollCause) => Promise<T>;
  describeError: (error: unknown) => string;
  /** A function is read before every pause, so the last answer can ask for an early next load. */
  intervalMs: number | (() => number);
  maxBackoffMs: number;
  staleMs?: number;
  visibility?: IVisibilitySource;
}

export interface ISharedResource<T> {
  subscribe: (listener: () => void) => () => void;
  getSnapshot: () => IResourceState<T>;
  getServerSnapshot: () => IResourceState<T>;
  refresh: () => Promise<void>;
  poke: (delayMs?: number) => void;
  /** Local change the server has already confirmed; the next load replaces it with the server's version. */
  update: (change: (current: T | null) => T | null) => void;
}

const EMPTY_STATE: IResourceState<never> = { data: null, error: null };

/**
 * Polls answer with fresh objects every time; keeping the old reference spares every subscriber a re-render.
 * A 304 hands back the very same object, which needs no serialising of two copies of a list on the main thread.
 */
const sameJson = (a: unknown, b: unknown) => a === b || JSON.stringify(a) === JSON.stringify(b);

export const createSharedResource = <T>(options: ISharedResourceOptions<T>): ISharedResource<T> => {
  let state: IResourceState<T> = EMPTY_STATE;
  const listeners = new Set<() => void>();

  const commit = (data: T | null, error: string | null, local = false) => {
    const nextData = data !== null && state.data !== null && sameJson(state.data, data) ? state.data : data;
    if (nextData === state.data && error === state.error) return;
    state = local ? { data: nextData, error, local: true } : { data: nextData, error };
    for (const listener of listeners) listener();
  };

  const poller = createPoller({
    intervalMs: options.intervalMs,
    maxBackoffMs: options.maxBackoffMs,
    staleMs: options.staleMs,
    visibility: options.visibility,
    run: async (signal, cause) => {
      try {
        const data = await options.load(signal, cause);
        if (!signal.aborted) commit(data, null);
      } catch (error) {
        if (!signal.aborted) commit(state.data, options.describeError(error));
        throw error;
      }
    },
  });

  return {
    subscribe: (listener) => {
      listeners.add(listener);
      if (listeners.size === 1) poller.start();
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) poller.stop();
      };
    },
    getSnapshot: () => state,
    getServerSnapshot: () => EMPTY_STATE,
    refresh: poller.refresh,
    poke: poller.poke,
    update: (change) => commit(change(state.data), state.error, true),
  };
};
