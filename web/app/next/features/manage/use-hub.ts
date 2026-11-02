import { useCallback, useEffect, useState } from 'react';
import { HubError, type ManageHub } from '@/next/features/manage/api';
import type { HubMe } from '@/next/features/manage/hub-types';

/** `none`: this runtime has no hub; `off`: the hub does not serve organizations; `loading` is not «empty». */
export type HubState =
  | { kind: 'none' }
  | { kind: 'loading' }
  | { kind: 'off' }
  | { kind: 'failed' }
  | { kind: 'ready'; me: HubMe };

export const useHub = (hub: ManageHub | null): { state: HubState; reload: () => void } => {
  const [loaded, setLoaded] = useState<{ hub: ManageHub; state: HubState } | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!hub) return;
    let alive = true;
    hub.load().then(
      (me) => alive && setLoaded({ hub, state: { kind: 'ready', me } }),
      (e: unknown) =>
        alive &&
        setLoaded({ hub, state: { kind: e instanceof HubError && e.kind === 'off' ? 'off' : 'failed' } }),
    );
    return () => {
      alive = false;
    };
  }, [hub, tick]);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  if (!hub) return { state: { kind: 'none' }, reload };
  return { state: loaded?.hub === hub ? loaded.state : { kind: 'loading' }, reload };
};
