import { appFetch, scopedAppPath } from '@/lib/app-path';
import { t } from '@/lib/i18n';

type TSessionAction = 'kill' | 'resume';

/** Kill or resume a conversation's agent on the machine it lives on; rejects with the server's reason. */
export const controlSession = async <T>(
  action: TSessionAction,
  tabId: string,
  machineId?: string,
): Promise<T> => {
  const response = await appFetch(scopedAppPath('/api/platform/session-process', machineId), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ tabId, action, machineId }),
  });
  const body = (await response.json().catch(() => null)) as (T & { error?: string }) | null;
  if (!response.ok || !body)
    throw new Error(body?.error ?? t('lib.hlSessionControl.error', { status: response.status }));
  return body;
};
