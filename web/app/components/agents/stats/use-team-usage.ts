import { useCallback, useEffect, useState } from 'react';
import { appFetch } from '@/lib/app-path';
import { t } from '@/lib/i18n';
import type { ITeamUsage } from '@/lib/hl-usage/types';

/** The runtime's usage report (`/api/platform/stats`), asked again every five minutes (ten seconds while it collects). */
export const useTeamUsage = () => {
  const [team, setTeam] = useState<ITeamUsage | null>(null);
  const [error, setError] = useState('');
  const [requested, setRequested] = useState(0);
  const [settled, setSettled] = useState(-1);

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await appFetch('/api/platform/stats');
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
  }, [requested]);

  const reload = useCallback(() => setRequested((count) => count + 1), []);

  useEffect(() => {
    if (!team) return undefined;
    const collecting = team.runtimes.some((r) => r.report?.collecting);
    const timer = window.setTimeout(reload, collecting ? 10_000 : 300_000);
    return () => window.clearTimeout(timer);
  }, [team, reload]);

  return { team, error, loading: settled !== requested, reload };
};
