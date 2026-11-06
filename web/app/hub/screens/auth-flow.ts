// The logic of sign-in and sign-up, apart from how it looks: the old screen (screens/auth.tsx) and the new hub's one
// (hx/auth/signin.tsx) both draw it. Providers appear as the hub's configuration says; e-mail is always there.
import { useEffect, useState } from 'react';

import { type ApiError, isApiError, oauthStartUrl } from '../lib/api';
import { useEnv } from '../lib/env';
import { errorText, signinRedirectError } from '../lib/errors';
import { isEmailLike } from '../lib/format';
import { useNow } from '../lib/hooks';
import { t } from '../lib/i18n';
import { navigate, safeNext, useQuery } from '../lib/router';
import { useSession } from '../lib/session';
import type { SignedIn } from '../lib/types';

export type AuthPhase = 'form' | 'sent' | 'entering';
export type AuthBusy = null | 'passkey' | 'send' | 'code';
export type OauthWay = { id: string; kind: 'github' | 'yandex' | 'oidc'; name: string };

const codeLeft = (e: ApiError): string =>
  e.attemptsLeft !== null ? t('auth.err.badCodeLeft', { n: e.attemptsLeft }) : t('auth.err.badCode');

export const useAuthFlow = (mode: 'signin' | 'signup') => {
  const env = useEnv();
  const { config, me, reload } = useSession();
  const query = useQuery();
  const next = safeNext(query.get('next'));
  const login = mode === 'signin';

  const [phase, setPhase] = useState<AuthPhase>('form');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState<AuthBusy>(null);
  const [error, setError] = useState<string | null>(null);
  const [codeBad, setCodeBad] = useState(false);
  const [resendAt, setResendAt] = useState(0);
  const now = useNow(1000, phase === 'sent');

  // Someone who is already in goes on to their space.
  useEffect(() => {
    if (me && phase !== 'entering') navigate(next ?? '/', { replace: true });
  }, [me, next, phase]);

  const oauth: OauthWay[] = [
    ...(config.auth.github ? [{ id: 'github', kind: 'github' as const, name: 'GitHub' }] : []),
    ...(config.auth.yandex ? [{ id: 'yandex', kind: 'yandex' as const, name: 'Яндекс' }] : []),
    ...config.auth.oidc.map((p) => ({ id: `oidc-${p.id}`, kind: 'oidc' as const, name: p.name })),
  ];
  const passkey = login && config.auth.passkey && env.passkeys.supported();

  const done = async (res: SignedIn) => {
    setPhase('entering');
    try {
      await reload();
    } catch {
      // the page loads /api/me again on its own route
    }
    navigate(next ?? res.next, { replace: true });
  };

  const fail = (e: unknown) => {
    setBusy(null);
    setError(errorText(e));
  };

  const send = async () => {
    const address = email.trim();
    if (!isEmailLike(address)) {
      setError(t('auth.badEmail'));
      return;
    }
    setError(null);
    setBusy('send');
    try {
      const r = await env.api.auth.emailStart(address, login ? 'signin' : 'signup');
      setEmail(address);
      setCode('');
      setCodeBad(false);
      setResendAt(env.now() + r.resendAfter * 1000);
      setPhase('sent');
      setBusy(null);
    } catch (e) {
      fail(e);
    }
  };

  const verify = async (digits: string) => {
    if (busy === 'code') return;
    setError(null);
    setCodeBad(false);
    setBusy('code');
    try {
      await done(await env.api.auth.emailVerify(email, digits));
    } catch (e) {
      setBusy(null);
      if (isApiError(e, 'invalid_code')) {
        setCode('');
        setCodeBad(true);
        setError(codeLeft(e));
      } else if (isApiError(e, 'expired')) {
        setCode('');
        setPhase('form');
        setError(t('auth.err.expired'));
      } else {
        setError(errorText(e));
      }
    }
  };

  const onCode = (digits: string) => {
    setCode(digits);
    setCodeBad(false);
    if (digits.length === 6) void verify(digits);
  };

  const signInWithPasskey = async () => {
    setError(null);
    setBusy('passkey');
    try {
      const begin = await env.api.auth.passkeyBegin();
      const credential = await env.passkeys.get(begin.publicKey);
      if (credential === null) {
        setBusy(null);
        return;
      }
      await done(await env.api.auth.passkeyFinish(begin.challengeId, credential));
    } catch (e) {
      setBusy(null);
      setError(isApiError(e, 'unauthenticated') ? t('auth.err.unknownPasskey') : errorText(e));
    }
  };

  const startOauth = (provider: string) =>
    env.assign(oauthStartUrl(provider, login ? 'signin' : 'signup', next ?? undefined));

  const changeEmail = () => {
    setPhase('form');
    setError(null);
    setCode('');
  };

  const switchTo = login ? '/signup' : '/signin';
  return {
    config,
    login,
    next,
    phase,
    email,
    setEmail,
    code,
    busy,
    error,
    codeBad,
    secondsLeft: Math.max(0, (resendAt - now) / 1000),
    redirectError: signinRedirectError(query.get('error')),
    oauth,
    passkey,
    send,
    verify,
    onCode,
    signInWithPasskey,
    startOauth,
    changeEmail,
    switchHref: next ? `${switchTo}?next=${encodeURIComponent(next)}` : switchTo,
  };
};
