// The one dialog's view of a resource, read when asked: null while it is read, `absent` where the gateway does
// not answer the new API (the older dialog stays then), `failed` where it is there but did not answer — the dialog says
// so with «Повторить» instead of falling back to the older one.
import { useCallback, useEffect, useState } from 'react';
import {
  fetchView,
  type IResourceView,
  type TResourceKind,
} from '@/next/features/designly/access/resource-api';

export const VIEW_STATE = { absent: 'absent', failed: 'failed' } as const;

export type TResourceState = IResourceView | (typeof VIEW_STATE)[keyof typeof VIEW_STATE] | null;

export const isResourceView = (state: TResourceState): state is IResourceView =>
  state !== null && typeof state === 'object';

export const useResourceView = (kind: TResourceKind, id: string, enabled: boolean) => {
  const [state, setState] = useState<TResourceState>(null);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    const abort = new AbortController();
    fetchView({ kind, id }, abort.signal).then(
      (view) => setState(view ?? VIEW_STATE.absent),
      () => {
        // What was read stays on screen; only nothing read yet turns into «failed».
        if (!abort.signal.aborted) setState((was) => (isResourceView(was) ? was : VIEW_STATE.failed));
      },
    );
    return () => abort.abort();
  }, [kind, id, enabled, nonce]);
  const reload = useCallback(() => {
    // A retry after a failure shows the skeleton again, not the error.
    setState((was) => (was === VIEW_STATE.failed ? null : was));
    setNonce((n) => n + 1);
  }, []);
  return { state, apply: setState as (next: IResourceView) => void, reload };
};
