import { appFetch } from '@/lib/app-path';
import type { TSlotTarget } from '@/lib/hl-preview-client';
import type { IVaultAuditEvent, IVaultEntry, TVaultStatus } from '@/types/vault';
import { t } from '@/lib/i18n';

/** Calls of the page to the safe; the gateway answers them itself, with the person's Teleport identity. */

const BASE = '/api/platform/vault';
/** Window event after the person changed the safe: «Sign in as …» buttons reload their entries. */
export const VAULT_CHANGED_EVENT = 'hl:vault-changed' as const;

export class VaultError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly needsApproval = false,
  ) {
    super(message);
  }
}

const call = async <T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> => {
  const response = await appFetch(`${BASE}${path}`, {
    method: init.method ?? 'GET',
    cache: 'no-store',
    credentials: 'same-origin',
    headers: {
      'x-hl-vault': '1',
      ...(init.body === undefined ? {} : { 'content-type': 'application/json' }),
    },
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  if (response.status === 204) return undefined as T;
  const body = (await response.json().catch(() => null)) as
    (T & { error?: unknown; needsApproval?: unknown }) | null;
  if (!response.ok) {
    const said =
      typeof body?.error === 'string' && body.error
        ? body.error
        : t('lib.hlVaultClient.error', { status: response.status });
    throw new VaultError(said, response.status, body?.needsApproval === true);
  }
  if (!body || typeof body !== 'object')
    throw new VaultError(t('lib.hlVaultClient.badReply'), response.status);
  return body as T;
};

export const vaultChanged = (): void => {
  window.dispatchEvent(new Event(VAULT_CHANGED_EVENT));
};

export const fetchVaultStatus = async (): Promise<TVaultStatus> => {
  try {
    const status = await call<TVaultStatus>('/status');
    if (status.enabled === true || (status.enabled === false && typeof status.reason === 'string'))
      return status;
    return { enabled: false, reason: t('lib.hlVaultClient.badReply') };
  } catch (error) {
    return {
      enabled: false,
      reason: error instanceof Error ? error.message : t('lib.hlVaultClient.unavailable'),
    };
  }
};

export const fetchVaultEntries = async (): Promise<IVaultEntry[]> => {
  const { entries } = await call<{ entries?: unknown }>('/entries');
  if (!Array.isArray(entries)) throw new VaultError(t('lib.hlVaultClient.badReply'), 200);
  return entries as IVaultEntry[];
};

export const fetchVaultAudit = async (): Promise<IVaultAuditEvent[]> =>
  (await call<{ events: IVaultAuditEvent[] }>('/audit')).events;

export interface IVaultDraft {
  origin: string;
  login: string;
  password: string;
  label: string;
  shared: boolean;
}

export const createVaultEntry = async (draft: IVaultDraft): Promise<IVaultEntry> =>
  (
    await call<{ entry: IVaultEntry }>('/entries', {
      method: 'POST',
      body: { ...draft, label: draft.label.trim() || null },
    })
  ).entry;

export const updateVaultEntry = async (
  id: string,
  change: { login?: string; password?: string; label?: string | null },
): Promise<IVaultEntry> =>
  (
    await call<{ entry: IVaultEntry }>(`/entries/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: change,
    })
  ).entry;

export const deleteVaultEntry = (id: string): Promise<void> =>
  call(`/entries/${encodeURIComponent(id)}`, { method: 'DELETE' });

export const revokeVaultGrants = (id: string): Promise<void> =>
  call(`/entries/${encodeURIComponent(id)}/grants`, { method: 'DELETE' });

export type TFillFor = { for: 'person' } | { for: 'agent'; agent: string; approval?: string };

/** A one-time token for one fill of one document of one slot; the gateway hands the value only to that slot's bridge. */
export const mintFillToken = async (
  id: string,
  origin: string,
  purpose: TFillFor,
  fill: { slot: string; nonce: string },
): Promise<string> => {
  const { token } = await call<{ token?: unknown }>(`/entries/${encodeURIComponent(id)}/fill`, {
    method: 'POST',
    body: { ...purpose, origin, ...fill },
  });
  if (typeof token !== 'string') throw new VaultError(t('lib.hlVaultClient.badReply'), 200);
  return token;
};

/** The person pressed «Allow»: a one-time approval for this entry, site and agent tab. */
export const approveVaultEntry = async (id: string, origin: string, agent: string): Promise<string> => {
  const { approval } = await call<{ approval?: unknown }>(`/entries/${encodeURIComponent(id)}/approve`, {
    method: 'POST',
    body: { origin, agent },
  });
  if (typeof approval !== 'string') throw new VaultError(t('lib.hlVaultClient.badReply'), 200);
  return approval;
};

export const denyVaultEntry = (id: string, agent: string): Promise<void> =>
  call(`/entries/${encodeURIComponent(id)}/deny`, { method: 'POST', body: { agent } });

/** The gateway of the preview names what the slot's cookie holds now; null when it would not say. */
export const fetchSlotTarget = async (slotOrigin: string): Promise<TSlotTarget | null> => {
  try {
    const response = await fetch(`${slotOrigin}/__tk/target`, { credentials: 'include', cache: 'no-store' });
    if (!response.ok) return null;
    const body = (await response.json()) as { target?: TSlotTarget };
    const target = body?.target;
    if (target?.kind === 'url' && typeof target.origin === 'string')
      return { kind: 'url', origin: target.origin };
    if (target?.kind === 'port' && Number.isInteger(target.port)) return { kind: 'port', port: target.port };
    return null;
  } catch {
    return null;
  }
};
