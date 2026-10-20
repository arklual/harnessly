import { oneLineSafe } from '@/lib/terminal-safe-text';
import { canonicalVaultOrigin } from '@/lib/vault/vault-origin';
import { t } from '@/lib/i18n';

/**
 * `vault login <origin>` of the agent CLI: the agent asks the page of the person to log in on a site with the safe.
 * The runtime carries the origin there and a status back; a value of the safe never passes through it.
 */

export const VAULT_REQUEST_EVENT = 'hl:vault-request' as const;

export const VAULT_LOGIN_STATUSES = [
  'logged-in',
  'not-logged-in',
  'needs-approval',
  'denied',
  'no-entry',
  'no-tab',
  'unavailable',
  'failed',
] as const;
export type TVaultLoginStatus = (typeof VAULT_LOGIN_STATUSES)[number];

export interface IVaultLoginResult {
  status: TVaultLoginStatus;
  message: string;
}

/** What the sync socket carries to every page of the runtime; only the page that shows `tabId` acts on it. */
export interface IVaultRequestEvent {
  type: 'vault-request';
  id: string;
  tabId: string;
  origin: string;
}

export type TVaultOutcome =
  | { kind: 'answered'; result: IVaultLoginResult }
  | { kind: 'no-ui' }
  | { kind: 'timeout' }
  | { kind: 'busy' }
  | { kind: 'rate-limited' };

const MAX_MESSAGE = 300;

export const vaultLoginOrigin = (value: unknown): string | null => canonicalVaultOrigin(value);

const isStatus = (value: unknown): value is TVaultLoginStatus =>
  typeof value === 'string' && (VAULT_LOGIN_STATUSES as readonly string[]).includes(value);

/** The page is trusted to say what happened, not how: the text reaches a terminal, so it is one safe line. */
export const parseVaultLoginResult = (value: unknown): IVaultLoginResult | null => {
  if (typeof value !== 'object' || value === null) return null;
  const { status, message } = value as Record<string, unknown>;
  if (!isStatus(status) || typeof message !== 'string') return null;
  return { status, message: oneLineSafe(message).slice(0, MAX_MESSAGE) };
};

export const vaultCliReply = (
  outcome: TVaultOutcome,
): { http: number; body: IVaultLoginResult | { error: string } } => {
  switch (outcome.kind) {
    case 'answered':
      return { http: 200, body: outcome.result };
    case 'timeout':
      return {
        http: 200,
        body: {
          status: 'needs-approval',
          message: t('lib.hlVaultRequest.timeout'),
        },
      };
    case 'no-ui':
      return {
        http: 503,
        body: { error: t('lib.hlVaultRequest.noUi') },
      };
    case 'busy':
      return { http: 409, body: { error: t('lib.hlVaultRequest.busy') } };
    case 'rate-limited':
      return { http: 429, body: { error: t('lib.hlVaultRequest.rateLimited') } };
  }
};
