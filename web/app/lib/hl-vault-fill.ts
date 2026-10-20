import type { TBridgeMessage } from '@/lib/hl-preview-bridge-client';
import type { TSlotTarget } from '@/lib/hl-preview-client';
import type { TFillReason, TParentMessage } from '@/lib/hl-preview/bridge-core';
import { sameVaultOrigin } from '@/lib/vault/vault-origin';
import { t } from '@/lib/i18n';

/** A login with the safe in the preview frame of a tab: the checks before the value exists, then fill and verify. */

export type TVaultRefusal = 'other-site' | 'no-bridge' | 'slot-switched' | 'no-answer' | TFillReason;

/** Mints a one-time fill token for this slot and document; the value itself goes to the bridge, never to this page. */
export type TVaultMint = (fill: { slot: string; nonce: string }) => Promise<string>;

export type TVaultFillOutcome =
  /** Submitted, and the next page has no password field. */
  | { kind: 'logged-in' }
  /** Submitted, and the page still asks for a password: a wrong one, or a second step. */
  | { kind: 'still-login' }
  /** Submitted, and the page did not answer the bridge afterwards. */
  | { kind: 'unverified' }
  /** Filled, but there was no form to submit. */
  | { kind: 'filled' }
  | { kind: 'refused'; reason: TVaultRefusal };

type TMessageOf<T extends TBridgeMessage['type']> = Extract<TBridgeMessage, { type: T }>;

export interface IVaultFillDeps {
  /** What the frame was opened on. */
  target: TSlotTarget;
  /** The slot origin of the frame. */
  slot: string;
  /** The nonce of the document the bridge last said hello from; null without a bridge. */
  nonce: string | null;
  /** What the slot's cookie names now, asked of the gateway; null when it would not say. */
  slotTarget: () => Promise<TSlotTarget | null>;
  /** Asked for only after every check passed. */
  mint: TVaultMint;
  send: (message: TParentMessage) => void;
  /** The next message of this type from the frame's bridge, or null after `timeoutMs`. */
  next: <T extends TBridgeMessage['type']>(type: T, timeoutMs: number) => Promise<TMessageOf<T> | null>;
}

const FILL_WAIT_MS = 5_000;
/** A submitted form navigates within this; a single-page login that does not is probed after it. */
const NAVIGATION_WAIT_MS = 4_000;
const PROBE_WAIT_MS = 2_000;

const refused = (reason: TVaultRefusal): TVaultFillOutcome => ({ kind: 'refused', reason });

const isSite = (target: TSlotTarget | null, origin: string): boolean =>
  target?.kind === 'url' && sameVaultOrigin(target.origin, origin);

export const performVaultLogin = async (origin: string, deps: IVaultFillDeps): Promise<TVaultFillOutcome> => {
  if (!isSite(deps.target, origin)) return refused('other-site');
  const { nonce } = deps;
  if (!nonce) return refused('no-bridge');
  if (!isSite(await deps.slotTarget(), origin)) return refused('slot-switched');
  const precheck = deps.next('tk:login-probe', PROBE_WAIT_MS);
  deps.send({ type: 'tk:probe-login' });
  const fit = await precheck;
  if (!fit) return refused('no-answer');
  if (fit.fit !== 'ok') return refused(fit.fit);
  const token = await deps.mint({ slot: deps.slot, nonce });
  const filled = deps.next('tk:filled', FILL_WAIT_MS);
  const navigated = deps.next('tk:hello', NAVIGATION_WAIT_MS);
  deps.send({ type: 'tk:fill', token });
  const result = await filled;
  if (!result) return refused('no-answer');
  if (!result.ok) return refused(result.reason ?? 'failed');
  if (!result.submitted) return { kind: 'filled' };
  await navigated;
  const probe = deps.next('tk:login-probe', PROBE_WAIT_MS);
  deps.send({ type: 'tk:probe-login' });
  const answer = await probe;
  if (!answer) return { kind: 'unverified' };
  return answer.passwordField ? { kind: 'still-login' } : { kind: 'logged-in' };
};

type TVaultLoginListener = (origin: string, mint: TVaultMint) => Promise<TVaultFillOutcome>;

/** The open preview frame of a tab takes logins; null when no frame of that tab is open. */
export const createVaultLoginChannel = () => {
  const byTab = new Map<string, TVaultLoginListener>();
  return {
    request: async (tab: string, origin: string, mint: TVaultMint): Promise<TVaultFillOutcome | null> => {
      const listener = byTab.get(tab);
      return listener ? listener(origin, mint) : null;
    },
    listen: (tab: string, listener: TVaultLoginListener): (() => void) => {
      byTab.set(tab, listener);
      return () => {
        if (byTab.get(tab) === listener) byTab.delete(tab);
      };
    },
  };
};

const channel = createVaultLoginChannel();

export const loginInPreview = (tab: string, origin: string, mint: TVaultMint) =>
  channel.request(tab, origin, mint);
export const onVaultLogin = (tab: string, listener: TVaultLoginListener): (() => void) =>
  channel.listen(tab, listener);

export const REFUSAL_TEXT: Record<TVaultRefusal, string> = {
  'other-site': t('lib.hlVaultFill.otherSite'),
  'no-bridge': t('lib.hlVaultFill.noBridge'),
  'slot-switched': t('lib.hlVaultFill.slotSwitched'),
  'no-answer': t('lib.hlVaultFill.noAnswer'),
  'no-form': t('lib.hlVaultFill.noForm'),
  'not-login-form': t('lib.hlVaultFill.notLoginForm'),
  'foreign-action': t('lib.hlVaultFill.foreignAction'),
  'service-worker': t('lib.hlVaultFill.serviceWorker'),
  expired: t('lib.hlVaultFill.expired'),
  moved: t('lib.hlVaultFill.moved'),
  failed: t('lib.hlVaultFill.failed'),
};
