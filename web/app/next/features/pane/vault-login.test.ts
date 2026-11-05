import { describe, expect, it } from 'vitest';
import type { TFrameMessage } from '@/next/features/pane/frame-bridge';
import type { TSlotTarget } from '@/next/core/net/vault';
import { performVaultLogin, type IVaultLoginDeps } from '@/next/features/pane/vault-login';

const SITE = 'https://app.example.com';
const NONCE = 'a1b2c3d4e5f60718293a4b5c6d7e8f90';

interface IFake {
  deps: IVaultLoginDeps;
  sent: Record<string, unknown>[];
  minted: { slot: string; nonce: string }[];
}

const probe = (passwordField: boolean, fit: 'ok' | 'no-form' = 'ok'): TFrameMessage => ({
  k: 'login-probe',
  passwordField,
  fit,
});

/** Answers by kind; a kind asked twice takes the next of its list. */
const fake = (
  answers: Record<string, (TFrameMessage | null)[]>,
  over: Partial<IVaultLoginDeps> = {},
): IFake => {
  const sent: Record<string, unknown>[] = [];
  const minted: { slot: string; nonce: string }[] = [];
  const queue = new Map(Object.entries(answers).map(([k, list]) => [k, [...list]]));
  const deps: IVaultLoginDeps = {
    target: SITE,
    slot: 'https://slot-7.preview.example.com',
    nonce: NONCE,
    slotTarget: async (): Promise<TSlotTarget | null> => ({ kind: 'url', origin: SITE }),
    mint: async (fill) => {
      minted.push(fill);
      return 'token-1';
    },
    send: (message) => void sent.push(message),
    next: (async (kind: string) => queue.get(kind)?.shift() ?? null) as IVaultLoginDeps['next'],
    ...over,
  };
  return { deps, sent, minted };
};

const hello: TFrameMessage = { k: 'hello', url: `${SITE}/home`, nonce: NONCE };

describe('a login from the safe', () => {
  it('logged in: submitted, and the next page has no password field', async () => {
    const f = fake({
      'login-probe': [probe(true), probe(false)],
      filled: [{ k: 'filled', ok: true, submitted: true, reason: null }],
      hello: [hello],
    });
    expect(await performVaultLogin(SITE, f.deps)).toEqual({ kind: 'logged-in' });
    expect(f.minted).toEqual([{ slot: f.deps.slot, nonce: NONCE }]);
    expect(f.sent.map((m) => m.type)).toEqual(['tk:probe-login', 'tk:fill', 'tk:probe-login']);
    expect(f.sent[1]).toEqual({ type: 'tk:fill', token: 'token-1' });
  });

  it('still on the login: a wrong password or a second step', async () => {
    const f = fake({
      'login-probe': [probe(true), probe(true)],
      filled: [{ k: 'filled', ok: true, submitted: true, reason: null }],
      hello: [hello],
    });
    expect(await performVaultLogin(SITE, f.deps)).toEqual({ kind: 'still-login' });
  });

  it('unverified: the page did not answer the second probe', async () => {
    const f = fake({
      'login-probe': [probe(true), null],
      filled: [{ k: 'filled', ok: true, submitted: true, reason: null }],
      hello: [null],
    });
    expect(await performVaultLogin(SITE, f.deps)).toEqual({ kind: 'unverified' });
  });

  it('filled: no form to submit', async () => {
    const f = fake({
      'login-probe': [probe(true)],
      filled: [{ k: 'filled', ok: true, submitted: false, reason: null }],
    });
    expect(await performVaultLogin(SITE, f.deps)).toEqual({ kind: 'filled' });
  });

  it('refuses before a token exists: another site, no bridge, a switched slot', async () => {
    const other = fake({}, { target: 'https://other.example.com' });
    expect(await performVaultLogin(SITE, other.deps)).toEqual({ kind: 'refused', reason: 'other-site' });
    const port = fake({}, { target: null });
    expect(await performVaultLogin(SITE, port.deps)).toEqual({ kind: 'refused', reason: 'other-site' });
    const bare = fake({}, { nonce: null });
    expect(await performVaultLogin(SITE, bare.deps)).toEqual({ kind: 'refused', reason: 'no-bridge' });
    const switched = fake(
      {},
      { slotTarget: async () => ({ kind: 'url', origin: 'https://other.example.com' }) },
    );
    expect(await performVaultLogin(SITE, switched.deps)).toEqual({
      kind: 'refused',
      reason: 'slot-switched',
    });
    const unsaid = fake({}, { slotTarget: async () => null });
    expect(await performVaultLogin(SITE, unsaid.deps)).toEqual({ kind: 'refused', reason: 'slot-switched' });
    for (const f of [other, port, bare, switched, unsaid]) expect(f.minted).toEqual([]);
  });

  it('refuses when the page does not answer the probe, or the form does not fit', async () => {
    const silent = fake({ 'login-probe': [null] });
    expect(await performVaultLogin(SITE, silent.deps)).toEqual({ kind: 'refused', reason: 'no-answer' });
    const nofit = fake({ 'login-probe': [probe(false, 'no-form')] });
    expect(await performVaultLogin(SITE, nofit.deps)).toEqual({ kind: 'refused', reason: 'no-form' });
    expect(silent.minted).toEqual([]);
    expect(nofit.minted).toEqual([]);
  });

  it('refuses when the fill is not answered or did not go', async () => {
    const silent = fake({ 'login-probe': [probe(true)], filled: [null] });
    expect(await performVaultLogin(SITE, silent.deps)).toEqual({ kind: 'refused', reason: 'no-answer' });
    const moved = fake({
      'login-probe': [probe(true)],
      filled: [{ k: 'filled', ok: false, submitted: false, reason: 'moved' }],
    });
    expect(await performVaultLogin(SITE, moved.deps)).toEqual({ kind: 'refused', reason: 'moved' });
    const bare = fake({
      'login-probe': [probe(true)],
      filled: [{ k: 'filled', ok: false, submitted: false, reason: null }],
    });
    expect(await performVaultLogin(SITE, bare.deps)).toEqual({ kind: 'refused', reason: 'failed' });
  });
});
