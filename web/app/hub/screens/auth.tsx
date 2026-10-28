// «Аккаунт»: sign-in and sign-up on one screen. Providers appear as the hub's configuration says; e-mail is always
// there. The e-mail code is six digits drawn as cells and sent by itself at the sixth.
import { useEffect, useState } from 'react';

import { Button, TextField } from '@kit';

import { type ApiError, isApiError, oauthStartUrl } from '../lib/api';
import { errorText, signinRedirectError } from '../lib/errors';
import { useEnv } from '../lib/env';
import { clock, isEmailLike } from '../lib/format';
import { useNow } from '../lib/hooks';
import { t } from '../lib/i18n';
import { Link, navigate, safeNext, useQuery } from '../lib/router';
import { useSession } from '../lib/session';
import type { SignedIn } from '../lib/types';
import { StepFrame } from '../ui/frame';
import { Fingerprint, GitBranch, LogIn, Mail, User } from '../ui/icons';
import { OtpInput } from '../ui/otp';
import { LinkButton, Msg, Note, Or } from '../ui/parts';
import { Spinner } from '@kit';

type Phase = 'form' | 'sent' | 'entering';
type Busy = null | 'passkey' | 'send' | 'code';

export const AuthScreen = ({ mode }: { mode: 'signin' | 'signup' }) => {
  const env = useEnv();
  const { config, me, reload } = useSession();
  const query = useQuery();
  const next = safeNext(query.get('next'));
  const login = mode === 'signin';

  const [phase, setPhase] = useState<Phase>('form');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState<Busy>(null);
  const [error, setError] = useState<string | null>(null);
  const [codeBad, setCodeBad] = useState(false);
  const [resendAt, setResendAt] = useState(0);
  const now = useNow(1000, phase === 'sent');

  // Someone who is already in goes on to their space.
  useEffect(() => {
    if (me && phase !== 'entering') navigate(next ?? '/', { replace: true });
  }, [me, next, phase]);

  const redirectError = signinRedirectError(query.get('error'));
  const oauth = [
    ...(config.auth.github
      ? [{ id: 'github', label: t('auth.viaGithub'), icon: <GitBranch size={16} /> }]
      : []),
    ...(config.auth.yandex
      ? [
          {
            id: 'yandex',
            label: t('auth.viaYandex'),
            icon: (
              <span className="hub-ya" aria-hidden="true">
                Я
              </span>
            ),
          },
        ]
      : []),
    ...config.auth.oidc.map((p) => ({
      id: `oidc-${p.id}`,
      label: t('auth.viaOidc', { name: p.name }),
      icon: <LogIn size={16} />,
    })),
  ];
  const passkey = login && config.auth.passkey && env.passkeys.supported();
  const hasOthers = passkey || oauth.length > 0;

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

  const left = Math.max(0, (resendAt - now) / 1000);
  const switchTo = login ? '/signup' : '/signin';
  const switchHref = next ? `${switchTo}?next=${encodeURIComponent(next)}` : switchTo;

  return (
    <StepFrame
      step={0}
      bare
      caption={login ? 'login' : undefined}
      icon={login ? <LogIn size={22} /> : <User size={22} />}
      title={login ? t('auth.signinTitle') : t('auth.signupTitle')}
      lead={login ? undefined : t('auth.lead')}
    >
      {redirectError && phase === 'form' && <Msg tone="danger">{redirectError}</Msg>}
      {!login && config.signup === 'invite' && phase === 'form' && (
        <Msg tone="info">{t('auth.inviteOnly')}</Msg>
      )}

      {phase === 'entering' && (
        <div className="hub-enter" role="status">
          <Spinner size="s" />
          {t('auth.entering')}
        </div>
      )}

      {phase === 'form' && (
        <div className="hub-stack">
          {passkey && (
            <>
              <Button
                appearance="primary"
                size="l"
                block
                loading={busy === 'passkey'}
                icon={<Fingerprint size={16} />}
                onClick={() => void signInWithPasskey()}
              >
                {busy === 'passkey' ? t('auth.passkeyBusy') : t('auth.passkey')}
              </Button>
              {oauth.length > 0 && <Or>{t('common.or')}</Or>}
            </>
          )}
          {oauth.length > 0 && (
            <div className="hub-row">
              {oauth.map((p) => (
                <Button key={p.id} size="l" data-grow="" icon={p.icon} onClick={() => startOauth(p.id)}>
                  {p.label}
                </Button>
              ))}
            </div>
          )}
          {hasOthers && <Or>{t('auth.orByMail')}</Or>}
          <form
            className="hub-inputrow"
            data-labeled
            onSubmit={(e) => {
              e.preventDefault();
              void send();
            }}
          >
            <TextField
              size="l"
              type="email"
              label={t('auth.mail')}
              autoComplete="email"
              placeholder="you@company.ru"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              error={error ?? undefined}
            />
            <Button
              type="submit"
              size="l"
              appearance={login ? 'outline' : 'primary'}
              loading={busy === 'send'}
            >
              {t('auth.getCode')}
            </Button>
          </form>
          {config.auth.email.mode === 'log' && <Note>{t('auth.logMode')}</Note>}
        </div>
      )}

      {phase === 'sent' && (
        <div className="hub-stack">
          <div className="hub-sent">
            <span className="hub-sent__ic">
              <Mail size={18} />
            </span>
            <span className="hub-sent__t">
              {t('auth.sentTo')}
              <b>{email}</b>
            </span>
            <Button
              size="xs"
              onClick={() => {
                setPhase('form');
                setError(null);
                setCode('');
              }}
            >
              {t('auth.change')}
            </Button>
          </div>
          <div className="hub-stack">
            <span className="hub-label">{t('auth.codeLabel')}</span>
            <OtpInput value={code} onChange={onCode} busy={busy === 'code'} invalid={codeBad} />
            {error && <Msg tone="danger">{error}</Msg>}
          </div>
          <Button
            appearance="primary"
            size="l"
            block
            loading={busy === 'code'}
            aria-disabled={code.length < 6 || undefined}
            onClick={() => code.length === 6 && void verify(code)}
          >
            {t('auth.continue')}
          </Button>
          <p className="hub-resend">
            {left > 0 ? (
              <>
                {t('auth.resendIn')} <b>{clock(left)}</b>
              </>
            ) : (
              <LinkButton onClick={() => void send()} disabled={busy === 'send'}>
                {t('auth.resend')}
              </LinkButton>
            )}
          </p>
          {config.auth.email.mode === 'log' && <Note>{t('auth.logMode')}</Note>}
        </div>
      )}

      {phase === 'form' && (
        <>
          <p className="hub-note">
            {login ? t('auth.noAccount') : t('auth.haveAccount')}{' '}
            <Link to={switchHref} className="hub-link">
              {login ? t('auth.create') : t('auth.signin')}
            </Link>
          </p>
          {config.termsUrl && (
            <p className="hub-note">
              {t('auth.termsBefore')}{' '}
              <a className="hub-link" href={config.termsUrl} target="_blank" rel="noreferrer">
                {t('auth.termsLink')}
              </a>
              .
            </p>
          )}
        </>
      )}
    </StepFrame>
  );
};

const codeLeft = (e: ApiError): string =>
  e.attemptsLeft !== null ? t('auth.err.badCodeLeft', { n: e.attemptsLeft }) : t('auth.err.badCode');
