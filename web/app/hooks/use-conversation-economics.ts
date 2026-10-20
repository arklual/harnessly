import { useEffect } from 'react';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import { createSharedResource, type ISharedResource } from '@/lib/shared-resource';
import type { IEconomicsData, IEconomicsResponse } from '@/types/server/economics';
import useSharedResource from '@/hooks/use-shared-resource';
import { t } from '@/lib/i18n';

export type { IEconomicsData, IEconomicsResponse };

const INTERVAL = {
  computing: 5_000,
  ready: 30_000,
} as const;

const sources = new Map<string, ISharedResource<IEconomicsResponse>>();

/**
 * One shared stream per conversation: the header chip and the inspector island of the same conversation reuse it.
 * `machineId` addresses a remote runtime from the shell; inside a runtime frame appFetch adds the frame's own.
 */
export const economicsSource = (tabId: string, machineId?: string): ISharedResource<IEconomicsResponse> => {
  const key = `${machineId ?? ''}:${tabId}`;
  const known = sources.get(key);
  if (known) return known;
  let computing = false;
  const source = createSharedResource<IEconomicsResponse>({
    load: async (signal, cause) => {
      const query = new URLSearchParams({ tab: tabId });
      if (cause === 'signal') query.set('fresh', '1');
      const response = await appFetch(scopedAppPath(`/api/platform/economics?${query}`, machineId), {
        signal,
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: string } | null;
        throw new Error(body?.error ?? `HTTP ${response.status}`);
      }
      const data = (await response.json()) as IEconomicsResponse;
      computing = data.conversation?.status === 'computing';
      return data;
    },
    describeError: (error) =>
      error instanceof Error ? error.message : t('hooks.useConversationEconomics.loadFailed'),
    intervalMs: () => (computing ? INTERVAL.computing : INTERVAL.ready),
    maxBackoffMs: 5 * 60_000,
  });
  sources.set(key, source);
  return source;
};

/**
 * Economics of one agent conversation; `data` is null while loading (never zeros). `activityKey` is any value
 * that changes when the conversation does something (new timeline entries): each change asks for fresh numbers.
 */
export const useConversationEconomics = (
  tabId: string | null,
  machineId?: string,
  activityKey?: string | number,
) => {
  const source = tabId ? economicsSource(tabId, machineId) : null;
  const state = useSharedResource(source);
  useEffect(() => {
    if (source && activityKey !== undefined) source.poke(3_000);
  }, [source, activityKey]);
  return state;
};
