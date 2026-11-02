import { useEffect, useState } from 'react';

import type { OrgApi } from './api';
import type { ShellData } from './types';

/** The shell data of an organization; reloads when the organization or the scope changes. */
export const useShellLoad = (api: OrgApi, org: string, scopeSlug: string) => {
  const [state, setState] = useState<{ data: ShellData | null; error: unknown }>({ data: null, error: null });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let alive = true;
    api.shell(org, scopeSlug).then(
      (data) => alive && setState({ data, error: null }),
      (error: unknown) => alive && setState({ data: null, error }),
    );
    return () => {
      alive = false;
    };
  }, [api, org, scopeSlug, tick]);
  return {
    ...state,
    reload: () => {
      setState({ data: null, error: null });
      setTick((n) => n + 1);
    },
  };
};
