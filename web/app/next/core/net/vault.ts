// The person's safe, as the browser tab of the pane asks it: whether it is on, which entries it holds for the person
// and a one-time token for one fill. The gateway answers with the person's own identity; a value never comes here.
import { gatewayPath } from '@/next/core/net/machine';
import { t } from '@/next/i18n';

/** Window event after the person changed the safe: whoever lists its entries asks again. */
export const VAULT_CHANGED_EVENT = 'hl:vault-changed' as const;

const PREFIX = '/vault';
const TIMEOUT_MS = 15_000;

/** The gateway's refusal in its own words, or the failure in ours. */
export class VaultError extends Error {}

export interface IVaultEntry {
  id: string;
  origin: string;
  login: string;
  label: string | null;
}

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;

const call = async (path: `/${string}`, init: { method?: string; body?: unknown } = {}): Promise<unknown> => {
  const ctl = new AbortController();
  const timer = window.setTimeout(() => ctl.abort(), TIMEOUT_MS);
  let res: Response;
  try {
    res = await fetch(gatewayPath(`${PREFIX}${path}`), {
      method: init.method ?? 'GET',
      cache: 'no-store',
      credentials: 'same-origin',
      signal: ctl.signal,
      headers: {
        'x-hl-vault': '1',
        ...(init.body === undefined ? {} : { 'content-type': 'application/json' }),
      },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    });
  } catch {
    throw new VaultError(ctl.signal.aborted ? t('next.failure.timeout') : t('next.failure.network'));
  } finally {
    window.clearTimeout(timer);
  }
  const body: unknown = await res.json().catch(() => null);
  if (!res.ok) {
    const said = isRecord(body) && typeof body.error === 'string' ? body.error : '';
    throw new VaultError(said || t('next.failure.http'));
  }
  return body;
};

/** Whether the safe is on for the person. */
export const fetchVaultEnabled = async (): Promise<boolean> => {
  const body = await call('/status');
  return isRecord(body) && body.enabled === true;
};

export const fetchVaultEntries = async (): Promise<IVaultEntry[]> => {
  const body = await call('/entries');
  if (!isRecord(body) || !Array.isArray(body.entries)) throw new VaultError(t('next.failure.http'));
  const entries: IVaultEntry[] = [];
  for (const raw of body.entries as unknown[]) {
    if (!isRecord(raw) || typeof raw.id !== 'string' || typeof raw.origin !== 'string') continue;
    if (typeof raw.login !== 'string') continue;
    entries.push({
      id: raw.id,
      origin: raw.origin,
      login: raw.login,
      label: typeof raw.label === 'string' && raw.label ? raw.label : null,
    });
  }
  return entries;
};

/** A one-time token for one fill of one document of one slot; the gateway hands the value only to that slot's bridge. */
export const mintFillToken = async (
  id: string,
  origin: string,
  fill: { slot: string; nonce: string },
  /** The machine whose slot shows the page: the gateway routes by it, as for the old interface. */
  machine: string,
): Promise<string> => {
  const body = await call(`/entries/${encodeURIComponent(id)}/fill?machine=${encodeURIComponent(machine)}`, {
    method: 'POST',
    body: { for: 'person', origin, ...fill },
  });
  if (!isRecord(body) || typeof body.token !== 'string') throw new VaultError(t('next.failure.http'));
  return body.token;
};

/** What the slot's cookie names now, asked of the slot's gateway; null when it would not say. */
export type TSlotTarget = { kind: 'url'; origin: string } | { kind: 'port'; port: number };

export const fetchSlotTarget = async (slotOrigin: string): Promise<TSlotTarget | null> => {
  try {
    const res = await fetch(`${slotOrigin}/__tk/target`, { credentials: 'include', cache: 'no-store' });
    if (!res.ok) return null;
    const body: unknown = await res.json();
    const target = isRecord(body) ? body.target : null;
    if (!isRecord(target)) return null;
    if (target.kind === 'url' && typeof target.origin === 'string')
      return { kind: 'url', origin: target.origin };
    if (target.kind === 'port' && typeof target.port === 'number' && Number.isInteger(target.port))
      return { kind: 'port', port: target.port };
    return null;
  } catch {
    return null;
  }
};
