import { VaultError, type TFillFor } from '@/lib/hl-vault-client';
import { REFUSAL_TEXT, type TVaultFillOutcome, type TVaultMint } from '@/lib/hl-vault-fill';
import type { IVaultLoginResult } from '@/lib/hl-vault-request';
import { sameVaultOrigin } from '@/lib/vault/vault-origin';
import type { IVaultEntry, TVaultStatus } from '@/types/vault';
import { t } from '@/lib/i18n';

/** What the page of the person does when the agent of its tab asks to log in somewhere. */

export interface IAgentLoginDeps {
  /** `<machine>/<tab>`: the grant the person gives is for this agent tab only. */
  agent: string;
  status: () => Promise<TVaultStatus>;
  entries: () => Promise<IVaultEntry[]>;
  /** The tab's browser shows this site now. */
  previewShows: (origin: string) => boolean;
  login: (origin: string, mint: TVaultMint) => Promise<TVaultFillOutcome | null>;
  mint: (id: string, purpose: TFillFor, fill: { slot: string; nonce: string }) => Promise<string>;
  /** A one-time approval of the person for this entry and agent tab. */
  approve: (id: string, agent: string) => Promise<string>;
  deny: (id: string, agent: string) => Promise<void>;
  /** The person's answer: the entry they allow, or null for «Decline» and no answer. */
  ask: (origin: string, entries: IVaultEntry[]) => Promise<IVaultEntry | null>;
}

class Denied extends Error {}

const result = (status: IVaultLoginResult['status'], message: string): IVaultLoginResult => ({
  status,
  message,
});

const fromOutcome = (outcome: TVaultFillOutcome | null, origin: string, login: string): IVaultLoginResult => {
  if (!outcome) return result('no-tab', t('lib.hlVaultAgent.openInBrowser', { origin }));
  switch (outcome.kind) {
    case 'logged-in':
      return result('logged-in', t('lib.hlVaultAgent.loggedIn', { origin, login }));
    case 'still-login':
      return result('not-logged-in', t('lib.hlVaultAgent.stillLogin'));
    case 'unverified':
      return result('not-logged-in', t('lib.hlVaultAgent.unverified'));
    case 'filled':
      return result('not-logged-in', t('lib.hlVaultAgent.filledNoForm'));
    case 'refused':
      return outcome.reason === 'other-site' || outcome.reason === 'slot-switched'
        ? result(
            'no-tab',
            t('lib.hlVaultAgent.openInBrowserBecause', { origin, reason: REFUSAL_TEXT[outcome.reason] }),
          )
        : result(
            'not-logged-in',
            t('lib.hlVaultAgent.notLoggedIn', { reason: REFUSAL_TEXT[outcome.reason] }),
          );
  }
};

export const runAgentLogin = async (origin: string, deps: IAgentLoginDeps): Promise<IVaultLoginResult> => {
  try {
    const status = await deps.status();
    if (!status.enabled) return result('unavailable', status.reason);
    const entries = (await deps.entries()).filter((entry) => sameVaultOrigin(entry.origin, origin));
    if (!entries.length) return result('no-entry', t('lib.hlVaultAgent.noEntry', { origin }));
    if (!deps.previewShows(origin)) return result('no-tab', t('lib.hlVaultAgent.openInBrowser', { origin }));

    let used = entries[0];
    const approved = async (candidates: IVaultEntry[], fill: { slot: string; nonce: string }) => {
      const chosen = await deps.ask(origin, candidates);
      if (!chosen) {
        await deps.deny(candidates[0].id, deps.agent).catch(() => undefined);
        throw new Denied();
      }
      used = chosen;
      const approval = await deps.approve(chosen.id, deps.agent);
      return deps.mint(chosen.id, { for: 'agent', agent: deps.agent, approval }, fill);
    };
    const mint: TVaultMint = async (fill) => {
      if (entries.length > 1) return approved(entries, fill);
      try {
        return await deps.mint(entries[0].id, { for: 'agent', agent: deps.agent }, fill);
      } catch (error) {
        if (error instanceof VaultError && error.needsApproval) return approved(entries, fill);
        throw error;
      }
    };
    const outcome = await deps.login(origin, mint);
    return fromOutcome(outcome, origin, used.login);
  } catch (error) {
    if (error instanceof Denied) return result('denied', t('lib.hlVaultAgent.denied'));
    return result(
      'failed',
      error instanceof Error
        ? t('lib.hlVaultAgent.vaultError', { reason: error.message })
        : t('lib.hlVaultAgent.vaultNoAnswer'),
    );
  }
};
