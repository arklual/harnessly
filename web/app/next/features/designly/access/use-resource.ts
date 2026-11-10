// The one dialog's view of a resource (HRN-675), read when asked: null while it is read, `absent` where the gateway does
// not answer the new API (the older dialog stays then).
import { useCallback, useEffect, useState } from 'react';
import {
  fetchView,
  type IResourceView,
  type TResourceKind,
} from '@/next/features/designly/access/resource-api';

export type TResourceState = IResourceView | 'absent' | null;

export const useResourceView = (kind: TResourceKind, id: string, enabled: boolean) => {
  const [state, setState] = useState<TResourceState>(null);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    const abort = new AbortController();
    fetchView({ kind, id }, abort.signal).then(
      (view) => setState(view ?? 'absent'),
      () => undefined,
    );
    return () => abort.abort();
  }, [kind, id, enabled, nonce]);
  const reload = useCallback(() => setNonce((n) => n + 1), []);
  return { state, apply: setState as (next: IResourceView) => void, reload };
};
