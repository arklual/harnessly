/** What the gateway's safe API answers; none of it ever carries a password. */

export type TVaultActor = 'person' | 'agent';

export interface IVaultEntry {
  id: string;
  origin: string;
  login: string;
  label: string | null;
  shared: boolean;
  /** The person may change or delete it: their own entry, or a shared one they edit. */
  mine: boolean;
  createdAt: string;
  updatedAt: string;
  lastUsedAt: string | null;
}

export type TVaultStatus =
  { enabled: true; canShare: boolean; grantHours: number } | { enabled: false; reason: string };

export interface IVaultAuditEvent {
  at: string;
  entryId: string | null;
  origin: string | null;
  action: string;
  actor: TVaultActor;
  agentKey: string | null;
  outcome: string;
}

export const VAULT_GRANT_HOURS = 8;
export const VAULT_MAX_LOGIN = 256;
export const VAULT_MAX_PASSWORD = 1024;
export const VAULT_MAX_LABEL = 120;
/** `<machine>/<tab id>`: which agent tab a grant is for. */
export const VAULT_AGENT_KEY = /^[A-Za-z0-9._:-]{1,96}\/[A-Za-z0-9._:-]{1,96}$/;
/** A one-time fill token or approval: 32 random bytes, base64url. */
export const VAULT_TOKEN = /^[A-Za-z0-9_-]{43}$/;
/** The in-page bridge's nonce of one document, 16 random bytes in hex. */
export const VAULT_FILL_NONCE = /^[0-9a-f]{32}$/;
