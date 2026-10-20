import { useMemo, useState } from 'react';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { createSharedResource, type ISharedResource } from '@/lib/shared-resource';
import type { ICommandCatalog } from '@/lib/hl-command-catalog/types';
import type { TAgentProvider } from '@/types/hl-launch';
import useSharedResource from '@/hooks/use-shared-resource';
import { t } from '@/lib/i18n';

/** One catalog stream per machine, agent and folder; kept across composers so reopening the list is instant. */
const g = globalThis as unknown as {
  __ptCommandCatalogSources?: Map<string, ISharedResource<ICommandCatalog>>;
};
if (!g.__ptCommandCatalogSources) g.__ptCommandCatalogSources = new Map();
const sources = g.__ptCommandCatalogSources;
/** The last catalog per machine and agent: shown while the list for a newly known folder loads. */
const lastCatalogs = new Map<string, ICommandCatalog>();

const catalogSource = (
  provider: TAgentProvider,
  machineId: string | undefined,
  cwd: string | null,
): ISharedResource<ICommandCatalog> => {
  const key = `${machineId ?? ''}\0${provider}\0${cwd ?? ''}`;
  const existing = sources.get(key);
  if (existing) return existing;
  let etag: string | null = null;
  let last: ICommandCatalog | null = null;
  const query = new URLSearchParams({ provider });
  if (cwd) query.set('cwd', cwd);
  const source = createSharedResource<ICommandCatalog>({
    load: async (signal) => {
      const response = await appFetch(scopedAppPath(`/api/platform/commands?${query}`, machineId), {
        signal,
        cache: 'no-store',
        headers: etag && last ? { 'If-None-Match': etag } : undefined,
      });
      if (response.status === 304 && last) return last;
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
        throw new Error(
          typeof body?.error === 'string'
            ? body.error
            : t('hooks.useCommandCatalog.loadFailedStatus', { status: response.status }),
        );
      }
      last = (await response.json()) as ICommandCatalog;
      etag = response.headers.get('ETag');
      lastCatalogs.set(`${machineId ?? ''}\0${provider}`, last);
      return last;
    },
    describeError: (error) =>
      error instanceof Error ? error.message : t('hooks.useCommandCatalog.loadFailed'),
    intervalMs: 5 * 60_000,
    maxBackoffMs: 5 * 60_000,
  });
  sources.set(key, source);
  return source;
};

export interface IUseCommandCatalog {
  /** `null` while loading or before the list was first asked for. */
  catalog: ICommandCatalog | null;
  error: string | null;
  retry: () => void;
}

/**
 * The slash-command catalog of one agent on one machine (the shell names the machine; inside a runtime frame
 * `appFetch` adds its own). Loads lazily: nothing is requested until `wanted` turns true once (the composer got
 * focus or a `/` was typed); from then on the catalog stays subscribed.
 */
const useCommandCatalog = (
  provider: TAgentProvider | null,
  machineId: string | undefined,
  cwd: string | null,
  wanted: boolean,
): IUseCommandCatalog => {
  const [latched, setLatched] = useState(false);
  if (wanted && !latched) setLatched(true);
  const source = useMemo(
    () => (provider && latched ? catalogSource(provider, machineId, cwd) : null),
    [provider, machineId, cwd, latched],
  );
  const state = useSharedResource(source);
  const previous = provider && latched ? (lastCatalogs.get(`${machineId ?? ''}\0${provider}`) ?? null) : null;
  return {
    catalog: state.data ?? (state.error ? null : previous),
    error: state.error,
    retry: () => void source?.refresh(),
  };
};

/** Drops cached catalogs after `/reload-plugins` or `/reload-skills` so the next list shows the change. */
export const refreshCommandCatalogs = (provider: TAgentProvider): void => {
  for (const [key, source] of sources) if (key.includes(`\0${provider}\0`)) void source.refresh();
};

export default useCommandCatalog;
