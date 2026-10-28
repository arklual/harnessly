// The two things every screen needs first: the hub's configuration and who is signed in (GET /api/config, GET
// /api/me; a 401 on the second is just "signed out").
import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import { type ApiError, isApiError } from './api';
import { useApi } from './env';
import type { Config, Me } from './types';

type Phase =
  { kind: 'loading' } | { kind: 'error'; error: ApiError } | { kind: 'ready'; config: Config; me: Me | null };

export interface Session {
  config: Config;
  me: Me | null;
  /** Reads /api/me again (after a sign-in, a new space, an accepted invite). */
  reload: () => Promise<Me | null>;
  /** Forgets the person (after a sign-out). */
  clear: () => void;
}

const SessionContext = createContext<Session | null>(null);

export const useSession = (): Session => {
  const s = useContext(SessionContext);
  if (!s) throw new Error('SessionProvider is missing');
  return s;
};

/** The signed-in person; for screens that are only reached signed in. */
export const useMe = (): Me => {
  const { me } = useSession();
  if (!me) throw new Error('not signed in');
  return me;
};

export const SessionProvider = ({
  children,
  failed,
  loading,
}: {
  children: ReactNode;
  failed: (error: ApiError, retry: () => void) => ReactNode;
  loading: ReactNode;
}) => {
  const api = useApi();
  const [phase, setPhase] = useState<Phase>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    void (async () => {
      try {
        const config = await api.config();
        const me = await api.me.get().catch((e: unknown) => {
          if (isApiError(e, 'unauthenticated')) return null;
          throw e;
        });
        if (alive) setPhase({ kind: 'ready', config, me });
      } catch (e) {
        if (alive) setPhase({ kind: 'error', error: isApiError(e) ? e : (e as ApiError) });
      }
    })();
    return () => {
      alive = false;
    };
  }, [api, attempt]);

  const reload = useCallback(async () => {
    const me = await api.me.get().catch((e: unknown) => {
      if (isApiError(e, 'unauthenticated')) return null;
      throw e;
    });
    setPhase((p) => (p.kind === 'ready' ? { ...p, me } : p));
    return me;
  }, [api]);

  const clear = useCallback(() => setPhase((p) => (p.kind === 'ready' ? { ...p, me: null } : p)), []);

  const value = useMemo<Session | null>(
    () => (phase.kind === 'ready' ? { config: phase.config, me: phase.me, reload, clear } : null),
    [phase, reload, clear],
  );

  if (phase.kind === 'loading') return <>{loading}</>;
  if (phase.kind === 'error')
    return (
      <>
        {failed(phase.error, () => {
          setPhase({ kind: 'loading' });
          setAttempt((n) => n + 1);
        })}
      </>
    );
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
};
