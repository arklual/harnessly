import { appConfig } from '@/platform/config';
import { useCallback, useEffect, useState } from 'react';
import { homeFetch as appFetch } from '@/lib/app-path';
import type { IEvalRuns } from '@/types/efficiency';
import { mineOpenAlerts } from '@/components/agents/efficiency/runs-math';
import { t } from '@/lib/i18n';

export type TEfficiencyStatus = 'loading' | 'ready' | 'pending' | 'error';

export interface IEfficiencyResource<T> {
  data: T | null;
  status: TEfficiencyStatus;
  error: string;
  /** A repeat request is in flight while the previous answer stays on screen. */
  refreshing: boolean;
  refresh: () => void;
}

const BASE = '/api/platform/efficiency';
/** What the hub answers itself (HRN-987): the summary and the weekly pulse, per organization, behind the `efficiency` flag. */
const HUB_PATHS = ['/summary', '/pulse'];
const isHubPath = (path: string): boolean => HUB_PATHS.some((p) => path === p || path.startsWith(`${p}?`));
const hubOrigin = (): string => appConfig().hubUrl ?? '';

let orgPromise: Promise<string | null> | null = null;
/** The organization the viewer's figures belong to: the first one the hub lists for them. */
const hubOrg = (): Promise<string | null> => {
  orgPromise ??= fetch(`${hubOrigin()}/api/orgs`, { credentials: 'include' })
    .then(async (res) => {
      if (!res.ok) throw new Error(String(res.status));
      const list = (await res.json()) as { orgs?: { slug?: string; id: string }[] };
      return list.orgs?.[0] ? (list.orgs[0].slug ?? list.orgs[0].id) : null;
    })
    .catch(() => {
      orgPromise = null;
      return null;
    });
  return orgPromise;
};

/** One request of the screen: the hub's endpoint for the summary and the pulse, the machine's gateway for the rest. */
const request = async (path: string, init?: RequestInit): Promise<Response> => {
  if (!isHubPath(path)) return appFetch(`${BASE}${path}`, init);
  const org = await hubOrg();
  // No organization or no hub to ask: the same «not there yet» the gateway gave before the scanner was deployed.
  if (!org) return new Response(null, { status: 404 });
  const hubPath = path.replace(/\?.*$/, '');
  return fetch(`${hubOrigin()}/api/orgs/${encodeURIComponent(org)}/efficiency${hubPath}`, {
    credentials: 'include',
    ...init,
  });
};
/** The backend answers these until the efficiency scanner is deployed: not a failure, the data is just not there yet. */
const NOT_DEPLOYED = new Set([404, 501]);
const ALERTS_POLL_MS = 300_000;

export const EFFICIENCY_ALERTS_EVENT = 'hl:efficiency-alerts-changed';

interface IState<T> {
  path: string;
  round: number;
  data: T | null;
  status: Exclude<TEfficiencyStatus, 'loading'>;
  error: string;
}

/** GET one efficiency endpoint. `path` is relative to /api/platform/efficiency and includes the query; null keeps the hook idle. */
export const useEfficiency = <T>(path: string | null): IEfficiencyResource<T> => {
  const [state, setState] = useState<IState<T> | null>(null);
  const [round, setRound] = useState(0);

  useEffect(() => {
    if (!path) return;
    const controller = new AbortController();
    const done = (next: Omit<IState<T>, 'path' | 'round'>) => setState({ path, round, ...next });
    request(path, { signal: controller.signal })
      .then(async (res) => {
        if (NOT_DEPLOYED.has(res.status)) return done({ data: null, status: 'pending', error: '' });
        if (!res.ok)
          return done({
            data: null,
            status: 'error',
            error:
              res.status === 403 ? t('hooks.useEfficiency.noAccess') : t('hooks.useEfficiency.loadFailed'),
          });
        return done({ data: (await res.json()) as T, status: 'ready', error: '' });
      })
      .catch(() => {
        if (controller.signal.aborted) return;
        done({ data: null, status: 'error', error: t('hooks.useEfficiency.loadFailed') });
      });
    return () => controller.abort();
  }, [path, round]);

  const refresh = useCallback(() => setRound((n) => n + 1), []);
  const current = path && state && state.path === path ? state : null;
  return {
    data: current?.data ?? null,
    status: current?.status ?? 'loading',
    error: current?.error ?? '',
    refreshing: !!path && !!current && current.round !== round,
    refresh,
  };
};

/** POST a change (a manual batch, a schedule switch, an alert state). Resolves to an error text, or null on success. */
export const postEfficiency = async (path: string, body: unknown): Promise<string | null> => {
  try {
    const res = await request(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (res.ok) return null;
    if (NOT_DEPLOYED.has(res.status)) return t('hooks.useEfficiency.runnerNotDeployed');
    return res.status === 403 ? t('hooks.useEfficiency.forbidden') : t('hooks.useEfficiency.saveFailed');
  } catch {
    return t('hooks.useEfficiency.saveFailed');
  }
};

/** Whether a regression addressed to the viewer is still open; drives the dot in the sidebar. Quiet on any failure. */
export const useMineAlert = (): boolean => {
  const [mine, setMine] = useState(false);
  const [round, setRound] = useState(0);

  useEffect(() => {
    if (!appConfig().features.efficiency) return;
    const controller = new AbortController();
    appFetch(`${BASE}/runs`, { signal: controller.signal })
      .then(async (res) => setMine(res.ok && mineOpenAlerts((await res.json()) as IEvalRuns).length > 0))
      .catch(() => undefined);
    const timer = window.setTimeout(() => setRound((n) => n + 1), ALERTS_POLL_MS);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [round]);

  useEffect(() => {
    const onChange = () => setRound((n) => n + 1);
    window.addEventListener(EFFICIENCY_ALERTS_EVENT, onChange);
    return () => window.removeEventListener(EFFICIENCY_ALERTS_EVENT, onChange);
  }, []);

  return mine;
};
