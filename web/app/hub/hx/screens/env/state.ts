// The environment view with optimistic writes. A change shows at once; when the hub refuses, only that change is taken
// back (so other changes still on their way stay) and the caller says why. When the last write has settled, the view is
// fetched again without a spinner, so what the hub counted replaces what the screen counted.
import { useCallback, useEffect, useRef, useState } from 'react';

import type { EnvResponse } from '../../../../hub-api/env';
import { useOrg } from '../../../org/context';

export type Result<T> = { ok: true; value: T } | { ok: false; error: unknown };

export interface Write<T> {
  /** The change as the screen shows it before the answer. */
  apply: (v: EnvResponse) => EnvResponse;
  /** Takes exactly that change back. */
  undo: (v: EnvResponse) => EnvResponse;
  run: () => Promise<T>;
  /** Folds the hub's answer into the view. */
  settle?: (v: EnvResponse, value: T) => EnvResponse;
}

export interface EnvView {
  view: EnvResponse | null;
  error: unknown;
  reload: () => void;
  write: <T>(w: Write<T>) => Promise<Result<T>>;
  /** The number of writes on their way. */
  pending: number;
}

export const useEnvView = (): EnvView => {
  const { api, shell, scope } = useOrg();
  const org = shell.org.slug;
  const key = scope.kind === 'org' ? 'all' : scope.tenant.slug;
  const [state, setState] = useState<{ view: EnvResponse | null; error: unknown }>({
    view: null,
    error: null,
  });
  const [pending, setPending] = useState(0);
  const [tick, setTick] = useState(0);
  const latest = useRef<EnvResponse | null>(null);
  const inFlight = useRef(0);
  const [seen, setSeen] = useState(`${org}/${key}`);

  if (seen !== `${org}/${key}`) {
    setSeen(`${org}/${key}`);
    setState({ view: null, error: null });
  }

  // Another scope starts empty: forget the view of the last one (a ref is not written while rendering).
  useEffect(() => {
    latest.current = null;
  }, [org, key]);

  const put = useCallback((view: EnvResponse | null) => {
    latest.current = view;
    setState((s) => ({ ...s, view, error: null }));
  }, []);

  useEffect(() => {
    let alive = true;
    api.envView(org, key).then(
      (view) => {
        if (alive && inFlight.current === 0) put(view);
      },
      (error: unknown) => {
        if (alive && latest.current === null) setState({ view: null, error });
      },
    );
    return () => {
      alive = false;
    };
  }, [api, org, key, tick, put]);

  const reload = useCallback(() => {
    latest.current = null;
    setState({ view: null, error: null });
    setTick((n) => n + 1);
  }, []);

  const write = useCallback(
    async <T>(w: Write<T>): Promise<Result<T>> => {
      const cur = latest.current;
      if (cur) put(w.apply(cur));
      inFlight.current += 1;
      setPending(inFlight.current);
      let out: Result<T>;
      try {
        const value = await w.run();
        const now = latest.current;
        if (now) put(w.settle ? w.settle(now, value) : now);
        out = { ok: true, value };
      } catch (error) {
        const now = latest.current;
        if (now) put(w.undo(now));
        out = { ok: false, error };
      }
      inFlight.current -= 1;
      setPending(inFlight.current);
      if (inFlight.current === 0) {
        // Silent refetch: the hub's own counts replace ours.
        api.envView(org, key).then(
          (view) => {
            if (inFlight.current === 0) put(view);
          },
          () => undefined,
        );
      }
      return out;
    },
    [api, org, key, put],
  );

  return { view: state.view, error: state.error, reload, write, pending };
};
