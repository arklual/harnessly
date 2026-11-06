// «Аккаунт»: sign-in and sign-up on one screen. Providers appear as the hub's configuration says; e-mail is always
// there. The e-mail code is six digits drawn as cells and sent by itself at the sixth.
import { Button, TextField } from '@kit';

import { clock } from '../lib/format';
import { t } from '../lib/i18n';
import { Link } from '../lib/router';
import { useAuthFlow } from './auth-flow';
import { StepFrame } from '../ui/frame';
import { Fingerprint, GitBranch, LogIn, Mail, User } from '../ui/icons';
import { OtpInput } from '../ui/otp';
import { LinkButton, Msg, Note, Or } from '../ui/parts';
import { Spinner } from '@kit';

export const AuthScreen = ({ mode }: { mode: 'signin' | 'signup' }) => {
  const f = useAuthFlow(mode);
  const { config, login, phase, email, code, busy, error, codeBad, redirectError, passkey } = f;
  const oauth = f.oauth.map((p) => ({
    id: p.id,
    label:
      p.kind === 'oidc'
        ? t('auth.viaOidc', { name: p.name })
        : t(p.kind === 'github' ? 'auth.viaGithub' : 'auth.viaYandex'),
    icon:
      p.kind === 'github' ? (
        <GitBranch size={16} />
      ) : p.kind === 'yandex' ? (
        <span className="hub-ya" aria-hidden="true">
          Я
        </span>
      ) : (
        <LogIn size={16} />
      ),
  }));
  const byMail = config.auth.email.mode !== 'off';
  const hasOthers = passkey || oauth.length > 0;
  const { send, verify, onCode, signInWithPasskey, startOauth, switchHref } = f;
  const left = f.secondsLeft;

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
          {byMail && hasOthers && <Or>{t('auth.orByMail')}</Or>}
          {byMail && (
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
                onChange={(e) => f.setEmail(e.target.value)}
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
          )}
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
            <Button size="xs" onClick={f.changeEmail}>
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
