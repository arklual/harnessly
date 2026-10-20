import { t } from '@/lib/i18n';
import { appHost } from '@/lib/site';
import { appFetch, scopedAppPath } from '@/lib/app-path';
import type { IDevtoolsSettings, IDevtoolsSnapshot, IServerLog } from '@/lib/hl-preview/devtools/types';

/** The runtime's DevTools record of a preview slot, read and changed from the panel. */

/** The slot's record lives on the machine that opened the preview: a board of another machine's library is there. */
const devtoolsPath = (
  path: string,
  slot: string,
  machineId: string | undefined,
  query: Record<string, string> = {},
) =>
  scopedAppPath(
    `/api/platform/preview/devtools${path}?${new URLSearchParams({ slot, ...query })}`,
    machineId,
  );

const RUNTIME_ERRORS: Record<string, string> = {
  Forbidden: t('lib.hlPreviewDevtoolsClient.sessionExpired', { host: appHost() }),
  'invalid slot': t('lib.hlPreviewDevtoolsClient.invalidSlot', { host: appHost() }),
};

/**
 * The runtime's own words when they are meant for a person, ours otherwise. Only the settings checks (a 400 on the
 * PATCH) are: the runtime writes them in the page's language. Its other answers are short machine tokens.
 */
const failure = async (response: Response, fallback: string, readable = false): Promise<Error> => {
  if (response.status === 404) return new Error(t('lib.hlPreviewDevtoolsClient.unavailable'));
  const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
  const said = typeof body?.error === 'string' ? body.error : '';
  const known = RUNTIME_ERRORS[said] ?? (readable && response.status === 400 && said ? said : null);
  return new Error(known ?? `${fallback} (${response.status})`);
};

export const fetchDevtools = async (
  slot: string,
  since: number,
  machineId: string | undefined,
  signal?: AbortSignal,
): Promise<IDevtoolsSnapshot> => {
  const response = await appFetch(devtoolsPath('', slot, machineId, { since: String(since) }), { signal });
  if (!response.ok) throw await failure(response, t('lib.hlPreviewDevtoolsClient.fetchFailed'));
  return (await response.json()) as IDevtoolsSnapshot;
};

export const patchDevtoolsSettings = async (
  slot: string,
  patch: Partial<IDevtoolsSettings>,
  machineId: string | undefined,
): Promise<IDevtoolsSettings> => {
  const response = await appFetch(devtoolsPath('', slot, machineId), {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(patch),
  });
  if (!response.ok) throw await failure(response, t('lib.hlPreviewDevtoolsClient.settingsFailed'), true);
  return ((await response.json()) as { settings: IDevtoolsSettings }).settings;
};

export const clearDevtools = async (slot: string, machineId: string | undefined): Promise<void> => {
  const response = await appFetch(devtoolsPath('', slot, machineId), { method: 'DELETE' });
  if (!response.ok) throw await failure(response, t('lib.hlPreviewDevtoolsClient.clearFailed'));
};

export const fetchServerLog = async (
  slot: string,
  all: boolean,
  machineId: string | undefined,
  signal?: AbortSignal,
): Promise<IServerLog> => {
  const response = await appFetch(devtoolsPath('/server-log', slot, machineId, all ? { all: '1' } : {}), {
    signal,
  });
  if (!response.ok) throw await failure(response, t('lib.hlPreviewDevtoolsClient.serverLogFailed'));
  return (await response.json()) as IServerLog;
};
