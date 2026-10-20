import { useCallback, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { createSharedResource, type ISharedResource } from '@/lib/shared-resource';
import type { ILaunchOptions } from '@/lib/hl-launch-options';
import type { ILaunchChoice, TAgentProvider } from '@/types/hl-launch';
import useSharedResource from '@/hooks/use-shared-resource';
import { t } from '@/lib/i18n';

const errorOf = async (response: Response, fallback: string): Promise<string> => {
  const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
  return typeof body?.error === 'string' ? body.error : `${fallback} (HTTP ${response.status})`;
};

/** One resource per machine and agent: the catalog only changes when the CLI refreshes its cache or settings. */
const g = globalThis as unknown as {
  __ptLaunchOptionsSources?: Map<string, ISharedResource<ILaunchOptions>>;
};
if (!g.__ptLaunchOptionsSources) g.__ptLaunchOptionsSources = new Map();
const sources = g.__ptLaunchOptionsSources;

const launchOptionsSource = (
  provider: TAgentProvider,
  machineId: string | undefined,
): ISharedResource<ILaunchOptions> => {
  const key = `${machineId ?? ''}:${provider}`;
  const existing = sources.get(key);
  if (existing) return existing;
  const source = createSharedResource<ILaunchOptions>({
    load: async (signal) => {
      const response = await appFetch(
        scopedAppPath(`/api/platform/launch-options?provider=${provider}`, machineId),
        { signal, cache: 'no-store' },
      );
      if (!response.ok) throw new Error(await errorOf(response, t('hooks.useLaunchOptions.loadFailed')));
      return (await response.json()) as ILaunchOptions;
    },
    describeError: (error) =>
      error instanceof Error ? error.message : t('hooks.useLaunchOptions.loadFailed'),
    intervalMs: 5 * 60_000,
    maxBackoffMs: 5 * 60_000,
  });
  sources.set(key, source);
  return source;
};

export interface IUseLaunchOptions {
  /** `null` while loading (or when there is no agent to choose for). */
  options: ILaunchOptions | null;
  error: string | null;
  /** The choice being saved right now; the saved value changes only after the machine confirms it. */
  pending: ILaunchChoice | null;
  save: (choice: ILaunchChoice) => Promise<boolean>;
  retry: () => void;
}

/**
 * Launch catalog and remembered choice of one agent on one machine. Saving is pessimistic: `pending` until the PATCH answers, then the resource takes the new value.
 */
const useLaunchOptions = (
  provider: TAgentProvider | null,
  machineId: string | undefined,
): IUseLaunchOptions => {
  const source = useMemo(
    () => (provider ? launchOptionsSource(provider, machineId) : null),
    [provider, machineId],
  );
  const state = useSharedResource(source);
  const [pending, setPending] = useState<ILaunchChoice | null>(null);

  const save = useCallback(
    async (choice: ILaunchChoice): Promise<boolean> => {
      if (!provider || !source) return false;
      setPending(choice);
      try {
        const response = await appFetch(scopedAppPath('/api/config', machineId), {
          method: 'PATCH',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ hlLaunchDefaults: { [provider]: choice } }),
        });
        if (!response.ok) {
          toast.error(await errorOf(response, t('hooks.useLaunchOptions.saveFailed')));
          return false;
        }
        source.update((current) => (current ? { ...current, saved: choice } : current));
        return true;
      } catch {
        toast.error(t('hooks.useLaunchOptions.machineNoAnswer'));
        return false;
      } finally {
        setPending(null);
      }
    },
    [machineId, provider, source],
  );

  const retry = useCallback(() => {
    void source?.refresh();
  }, [source]);

  return { options: state.data, error: state.error, pending, save, retry };
};

export default useLaunchOptions;
