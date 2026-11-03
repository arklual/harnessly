// The teams view with optimistic actions: an action changes the screen at once, goes to the hub in turn, and when the
// hub refuses it the change is taken back and the reason is said. The hub's own answer replaces the local guess.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import type { TeamsView } from '../../../org/api/teams';
import type { OrgApi } from '../../../org/api';
import { useOrg } from '../../../org/context';

export interface Op {
  /** The screen's guess of the view after the action. */
  patch: (v: TeamsView) => TeamsView;
  /** The call to the hub; answers its view. */
  run: (api: OrgApi, org: string, scope: string) => Promise<TeamsView>;
}

export interface TeamsState {
  /** The view as it will be when every action is done; null while loading. */
  view: TeamsView | null;
  error: unknown;
  reload: () => void;
  /** Runs an action; resolves true when the hub accepted it. */
  apply: (op: Op) => Promise<boolean>;
  /** Actions on their way to the hub. */
  busy: boolean;
  /** Why the last action was refused. */
  problem: string | null;
  clearProblem: () => void;
}

export const useTeams = (slug: string): TeamsState => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const key = `${org}/${slug}`;
  const [state, setState] = useState<{ key: string; base: TeamsView | null; error: unknown }>({
    key,
    base: null,
    error: null,
  });
  const [pending, setPending] = useState<{ key: string; ops: Op[] }>({ key, ops: [] });
  const [problem, setProblem] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const queue = useRef<Promise<unknown>>(Promise.resolve());
  const current = useRef(key);
  useEffect(() => {
    current.current = key;
  });

  useEffect(() => {
    let alive = true;
    api.teamsView(org, slug).then(
      (base) => alive && setState({ key, base, error: null }),
      (error: unknown) => alive && setState({ key, base: null, error }),
    );
    return () => {
      alive = false;
    };
  }, [api, org, slug, key, tick]);

  const reload = useCallback(() => {
    setState((s) => ({ key: s.key, base: null, error: null }));
    setTick((n) => n + 1);
  }, []);

  const apply = useCallback(
    (op: Op): Promise<boolean> => {
      const at = key;
      setProblem(null);
      setPending((p) => ({ key: at, ops: p.key === at ? [...p.ops, op] : [op] }));
      const done = queue.current.then(async () => {
        let ok = false;
        try {
          const next = await op.run(api, org, slug);
          if (current.current === at) setState({ key: at, base: next, error: null });
          ok = true;
        } catch (e) {
          if (current.current === at) setProblem(orgErrorText(e));
        } finally {
          setPending((p) => ({ key: p.key, ops: p.ops.filter((x) => x !== op) }));
        }
        return ok;
      });
      queue.current = done;
      return done;
    },
    [api, org, slug, key],
  );

  const base = state.key === key ? state.base : null;
  const ops = useMemo(() => (pending.key === key ? pending.ops : []), [pending, key]);
  const view = useMemo(() => (base ? ops.reduce((v, op) => op.patch(v), base) : null), [base, ops]);
  return {
    view,
    error: state.key === key ? state.error : null,
    reload,
    apply,
    busy: ops.length > 0,
    problem,
    clearProblem: useCallback(() => setProblem(null), []),
  };
};
