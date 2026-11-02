// Data hooks of the organization hub. A load is `null` until it arrives (never `[]` — empty is a result, not a wait);
// a failure stays in `error` and is drawn, never replaced by stand-in data. A mutation is pessimistic: `pending` while
// it runs, the screen changes only after it succeeds.
import { useCallback, useEffect, useState } from 'react';

import { type OrgApi } from './api';
import { useOrg } from './context';

export interface Loaded<T> {
  data: T | null;
  error: unknown;
  reload: () => void;
  /** Fetches again without clearing what is shown. */
  refresh: () => void;
}

export const useLoad = <T>(load: (api: OrgApi) => Promise<T>, deps: readonly unknown[]): Loaded<T> => {
  const { api } = useOrg();
  const [state, setState] = useState<{ data: T | null; error: unknown }>({ data: null, error: null });
  const [tick, setTick] = useState(0);
  // New inputs: forget the old answer during render, so the screen shows "loading" and not a stale result.
  const depsKey = JSON.stringify(deps);
  const [seen, setSeen] = useState(depsKey);
  if (seen !== depsKey) {
    setSeen(depsKey);
    setState({ data: null, error: null });
  }
  useEffect(() => {
    let alive = true;
    load(api).then(
      (data) => alive && setState({ data, error: null }),
      (error: unknown) => alive && setState({ data: null, error }),
    );
    return () => {
      alive = false;
    };
    // `deps` (as depsKey) say when to refetch; `load` is the caller's closure over them.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, tick, depsKey]);
  const reload = useCallback(() => {
    setState({ data: null, error: null });
    setTick((n) => n + 1);
  }, []);
  const refresh = useCallback(() => setTick((n) => n + 1), []);
  return { ...state, reload, refresh };
};

export interface Action<A extends unknown[], R> {
  run: (...args: A) => Promise<R | undefined>;
  pending: boolean;
  error: unknown;
  reset: () => void;
}

export const useAction = <A extends unknown[], R>(
  fn: (api: OrgApi, ...args: A) => Promise<R>,
): Action<A, R> => {
  const { api } = useOrg();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const run = useCallback(
    async (...args: A) => {
      setPending(true);
      setError(null);
      try {
        return await fn(api, ...args);
      } catch (e) {
        setError(e);
        return undefined;
      } finally {
        setPending(false);
      }
    },
    [api, fn],
  );
  return { run, pending, error, reset: () => setError(null) };
};
