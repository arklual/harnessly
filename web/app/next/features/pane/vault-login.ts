// A login with the safe in the page of a browser tab: the checks before the value exists, then fill and verify. The
// password goes from the gateway to the page's bridge; this side holds a one-time token and nothing else.
import { sameVaultOrigin } from '@/next/features/pane/vault-origin';
import type { TFillReason, TFrameMessage } from '@/next/features/pane/frame-bridge';
import type { TSlotTarget } from '@/next/core/net/vault';

export type TVaultRefusal = 'other-site' | 'no-bridge' | 'slot-switched' | 'no-answer' | TFillReason;

export const VAULT_OUTCOME = {
  /** Submitted, and the next page has no password field. */
  loggedIn: 'logged-in',
  /** Submitted, and the page still asks for a password: a wrong one, or a second step. */
  stillLogin: 'still-login',
  /** Submitted, and the page did not answer the bridge afterwards. */
  unverified: 'unverified',
  /** Filled, but there was no form to submit. */
  filled: 'filled',
  refused: 'refused',
} as const;

export type TVaultOutcome =
  | { kind: typeof VAULT_OUTCOME.loggedIn }
  | { kind: typeof VAULT_OUTCOME.stillLogin }
  | { kind: typeof VAULT_OUTCOME.unverified }
  | { kind: typeof VAULT_OUTCOME.filled }
  | { kind: typeof VAULT_OUTCOME.refused; reason: TVaultRefusal };

type TMessageOf<K extends TFrameMessage['k']> = Extract<TFrameMessage, { k: K }>;

export interface IVaultLoginDeps {
  /** What the tab shows: the site's origin; null for a server of the machine. */
  target: string | null;
  /** The slot origin of the frame. */
  slot: string;
  /** The nonce of the document the bridge last said hello from; null without a bridge. */
  nonce: string | null;
  /** What the slot's cookie names now; null when it would not say. */
  slotTarget: () => Promise<TSlotTarget | null>;
  /** Asked for only after every check passed. */
  mint: (fill: { slot: string; nonce: string }) => Promise<string>;
  send: (message: Record<string, unknown>) => void;
  /** The next message of this kind from the frame's bridge, or null after `timeoutMs`. */
  next: <K extends TFrameMessage['k']>(kind: K, timeoutMs: number) => Promise<TMessageOf<K> | null>;
}

const FILL_WAIT_MS = 5_000;
/** A submitted form navigates within this; a single-page login that does not is probed after it. */
const NAVIGATION_WAIT_MS = 4_000;
const PROBE_WAIT_MS = 2_000;

const refused = (reason: TVaultRefusal): TVaultOutcome => ({ kind: VAULT_OUTCOME.refused, reason });

const isSite = (target: TSlotTarget | null, origin: string): boolean =>
  target?.kind === 'url' && sameVaultOrigin(target.origin, origin);

export const performVaultLogin = async (origin: string, deps: IVaultLoginDeps): Promise<TVaultOutcome> => {
  if (!sameVaultOrigin(deps.target, origin)) return refused('other-site');
  const { nonce } = deps;
  if (!nonce) return refused('no-bridge');
  if (!isSite(await deps.slotTarget(), origin)) return refused('slot-switched');
  const precheck = deps.next('login-probe', PROBE_WAIT_MS);
  deps.send({ type: 'tk:probe-login' });
  const fit = await precheck;
  if (!fit) return refused('no-answer');
  if (fit.fit !== 'ok') return refused(fit.fit);
  const token = await deps.mint({ slot: deps.slot, nonce });
  const filled = deps.next('filled', FILL_WAIT_MS);
  const navigated = deps.next('hello', NAVIGATION_WAIT_MS);
  deps.send({ type: 'tk:fill', token });
  const result = await filled;
  if (!result) return refused('no-answer');
  if (!result.ok) return refused(result.reason ?? 'failed');
  if (!result.submitted) return { kind: VAULT_OUTCOME.filled };
  await navigated;
  const probe = deps.next('login-probe', PROBE_WAIT_MS);
  deps.send({ type: 'tk:probe-login' });
  const answer = await probe;
  if (!answer) return { kind: VAULT_OUTCOME.unverified };
  return answer.passwordField ? { kind: VAULT_OUTCOME.stillLogin } : { kind: VAULT_OUTCOME.loggedIn };
};
