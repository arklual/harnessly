import { useCallback, useEffect, useState } from 'react';
import { appFetch } from '@/lib/app-path';
import { t } from '@/lib/i18n';
import type { ITeamUsage } from '@/lib/hl-usage/types';

/**
 * The runtime's usage report (`/api/platform/stats`), asked again every five minutes (ten seconds while it collects).
 * With `hubBase` (RUNTIME_HUB_ANALYTICS: the runtime counts nothing) the report is the hub's, `<hub>/api/me/stats`, in the
 * same shape.
 */
export const useTeamUsage = (skip = false, hubBase: string | null = null) => {
  const [team, setTeam] = useState<ITeamUsage | null>(null);
  const [error, setError] = useState('');
  const [requested, setRequested] = useState(0);
  const [settled, setSettled] = useState(-1);

  useEffect(() => {
    // The hub's figures are on the screen: the runtime's report is neither asked for nor built.
    if (skip) return undefined;
    let cancelled = false;
    const load = async () => {
      try {
        const res = hubBase
          ? await fetch(`${hubBase}/api/me/stats`, { credentials: 'include' })
          : await appFetch('/api/platform/stats');
        if (!res.ok)
          throw new Error(
            res.status === 403 ? t('stats.statsView.errorNoAccess') : t('stats.statsView.errorLoad'),
          );
        const data = (await res.json()) as ITeamUsage;
        if (cancelled) return;
        setTeam(data);
        setError('');
      } catch (e) {
        if (!cancelled) setError(e instanceof Error ? e.message : t('stats.statsView.errorLoad'));
      } finally {
        if (!cancelled) setSettled(requested);
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [requested, skip, hubBase]);

  const reload = useCallback(() => setRequested((count) => count + 1), []);

  useEffect(() => {
    if (skip || !team) return undefined;
    const collecting = team.runtimes.some((r) => r.report?.collecting);
    const timer = window.setTimeout(reload, collecting ? 10_000 : 300_000);
    return () => window.clearTimeout(timer);
  }, [team, reload, skip]);

  if (skip) return { team: null, error: '', loading: false, reload };
  return { team, error, loading: settled !== requested, reload };
};
