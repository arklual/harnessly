// The project's access as the gateway tells it: loading, absent (no gateway here: nothing to share or ask) or ready.
// A change answers with the fresh state, which replaces the one on screen only after the gateway has said so.
import { useCallback, useEffect, useState } from 'react';
import { fetchAccess, type IProjectAccess } from '@/next/features/designly/access/api';

export const ACCESS = { loading: 'loading', absent: 'absent', ready: 'ready' } as const;

export type TAccessState =
  | { k: typeof ACCESS.loading }
  | { k: typeof ACCESS.absent }
  | { k: typeof ACCESS.ready; access: IProjectAccess };

export interface IProjectAccessHook {
  state: TAccessState;
  /** Puts the gateway's fresh answer on screen. */
  apply: (next: IProjectAccess) => void;
  /** Asks the gateway again, quietly: what is on screen stays until the answer. */
  reload: () => void;
}

export const useProjectAccess = (project: string): IProjectAccessHook => {
  const [state, setState] = useState<TAccessState>({ k: ACCESS.loading });
  const [round, setRound] = useState(0);

  useEffect(() => {
    const abort = new AbortController();
    fetchAccess(project, abort.signal).then(
      (access) => setState(access ? { k: ACCESS.ready, access } : { k: ACCESS.absent }),
      () => undefined,
    );
    return () => abort.abort();
  }, [project, round]);

  const apply = useCallback((next: IProjectAccess) => setState({ k: ACCESS.ready, access: next }), []);
  const reload = useCallback(() => setRound((n) => n + 1), []);
  return { state, apply, reload };
};
