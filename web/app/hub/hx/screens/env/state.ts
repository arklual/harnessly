// The data of the screen. Loading is `null` (a list that came back empty is `[]`). A change goes to the hub first and the
// screen shows what the hub kept: after it the view is fetched again, and only then does the caller say it is done.
import { useCallback, useEffect, useRef, useState } from 'react';

import type { CatalogItem, EnvLayer, EnvResponse, MeMachine } from '../../../../hub-api/env';
import { useOrg } from '../../../org/context';
import type { Target } from './scope';
import { layersOfMe } from './scope';

export interface EnvData {
  /** The layers (a member's: what their team gets); null while loading. */
  layers: EnvLayer[] | null;
  /** The teams of the layers, for the picker's counts (a member has none). */
  view: EnvResponse | null;
  machines: MeMachine[];
  outdated: boolean;
  error: unknown;
  reload: () => void;
  /** Fetches again behind the screen and answers when the new data is in. */
  refresh: () => Promise<void>;
}

export const useEnvData = (): EnvData => {
  const { api, shell, scope } = useOrg();
  const org = shell.org.slug;
  const member = shell.viewer.role === 'member';
  const key = `${org}/${member ? 'me' : 'org'}/${scope.kind === 'org' ? 'all' : scope.tenant.slug}`;
  const space = scope.kind === 'tenant' ? scope.tenant.slug : undefined;
  const [state, setState] = useState<{
    view: EnvResponse | null;
    layers: EnvLayer[] | null;
    machines: MeMachine[];
    outdated: boolean;
    error: unknown;
  }>({ view: null, layers: null, machines: [], outdated: false, error: null });
  const [seen, setSeen] = useState(key);
  const [tick, setTick] = useState(0);
  const alive = useRef(true);

  if (seen !== key) {
    setSeen(key);
    setState({ view: null, layers: null, machines: [], outdated: false, error: null });
  }

  const fetchNow = useCallback(async () => {
    if (member) {
      const me = await api.envMine(space);
      return { view: null, layers: layersOfMe(me), machines: me.machines, outdated: me.outdated };
    }
    const view = await api.envView(org, scope.kind === 'org' ? 'all' : scope.tenant.slug);
    return { view, layers: view.layers, machines: [], outdated: false };
  }, [api, org, member, space, scope]);

  useEffect(() => {
    alive.current = true;
    fetchNow().then(
      (d) => {
        if (alive.current) setState({ ...d, error: null });
      },
      (error: unknown) => {
        if (alive.current) setState((s) => ({ ...s, error }));
      },
    );
    return () => {
      alive.current = false;
    };
  }, [fetchNow, tick]);

  const reload = useCallback(() => {
    setState({ view: null, layers: null, machines: [], outdated: false, error: null });
    setTick((n) => n + 1);
  }, []);

  const refresh = useCallback(async () => {
    const d = await fetchNow();
    if (alive.current) setState({ ...d, error: null });
  }, [fetchNow]);

  return { ...state, reload, refresh };
};

/** The organization's catalog, read once for the layer the person is setting up (a manager's right). */
export const useCatalog = (enabled: boolean, target: Target | null) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  const [items, setItems] = useState<CatalogItem[] | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [tick, setTick] = useState(0);
  const layer = target?.layer;
  const scopeId = target?.scopeId;
  useEffect(() => {
    if (!enabled || !layer || !scopeId) return;
    let live = true;
    api.envCatalog(org, { layer, scopeId }).then(
      (list) => {
        if (live) {
          setItems(list);
          setError(null);
        }
      },
      (e: unknown) => {
        if (live) setError(e);
      },
    );
    return () => {
      live = false;
    };
  }, [api, org, enabled, layer, scopeId, tick]);
  const refresh = useCallback(async () => {
    if (!layer || !scopeId) return;
    setItems(await api.envCatalog(org, { layer, scopeId }));
  }, [api, org, layer, scopeId]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  return { items, error, refresh, reload };
};
