// /signin and /signup on the new hub's kit. The logic is the old screen's (screens/auth-flow.ts); the buttons come from
// /api/config: passkey, GitHub, Яндекс, every OpenID provider and, always, e-mail.
import { clock } from '../../lib/format';
import { t } from '../../lib/i18n';
import { Link } from '../../lib/router';
import { useAuthFlow } from '../../screens/auth-flow';
import { Btn, Icon } from '../ui';
import { AuthFrame, Note, Or, Problem } from './frame';
import { HxOtp } from './otp';

export const HxAuth = ({ mode }: { mode: 'signin' | 'signup' }) => {
  const f = useAuthFlow(mode);
  const { config, login, phase, email, code, busy, error, codeBad, redirectError } = f;
  const hasOthers = f.passkey || f.oauth.length > 0;
  const byMail = config.auth.email.mode !== 'off';
  const closed = !login && config.signup === 'invite';

  if (phase === 'entering') {
    return (
      <AuthFrame title={t('auth.entering')}>
        <div className="hxa__center" role="status">
          <Icon i="loader-circle" />
        </div>
      </AuthFrame>
    );
  }

  return (
    <AuthFrame
      title={login ? t('auth.signinTitle') : t('auth.signupTitle')}
      lead={login || phase === 'sent' ? undefined : t('auth.lead')}
      footer={
        phase === 'form' && (
          <div className="hxa__stack">
            <p className="hxa__note">
              {login ? t('auth.noAccount') : t('auth.haveAccount')}{' '}
              <Link to={f.switchHref}>{login ? t('auth.create') : t('auth.signin')}</Link>
            </p>
            {config.termsUrl && (
              <p className="hxa__note">
                {t('auth.termsBefore')}{' '}
                <a href={config.termsUrl} target="_blank" rel="noreferrer">
                  {t('auth.termsLink')}
                </a>
                .
              </p>
            )}
          </div>
        )
      }
    >
      {phase === 'form' && closed && !hasOthers ? (
        <Problem
          title={t('authx.closed.title')}
          text={t('authx.closed.text')}
          actions={
            <Link to={f.switchHref} className="hx-btn" data-variant="primary">
              {t('auth.signin')}
            </Link>
          }
        />
      ) : null}
      {phase === 'form' && !(closed && !hasOthers) && (
        <>
          {redirectError && <Note tone="danger">{redirectError}</Note>}
          {closed && <Note>{t('auth.inviteOnly')}</Note>}
          {f.passkey && (
            <Btn
              variant="primary"
              size="lg"
              data-block=""
              icon="key-round"
              busy={busy === 'passkey'}
              onClick={() => void f.signInWithPasskey()}
            >
              {busy === 'passkey' ? t('auth.passkeyBusy') : t('auth.passkey')}
            </Btn>
          )}
          {f.passkey && f.oauth.length > 0 && <Or>{t('common.or')}</Or>}
          {f.oauth.length > 0 && (
            <div className="hxa__row">
              {f.oauth.map((p) => (
                <Btn
                  key={p.id}
                  size="lg"
                  icon={p.kind === 'github' ? 'git-branch' : p.kind === 'oidc' ? 'log-in' : undefined}
                  onClick={() => f.startOauth(p.id)}
                >
                  {p.kind === 'oidc' ? t('auth.viaOidc', { name: p.name }) : p.name}
                </Btn>
              ))}
            </div>
          )}
          {byMail && hasOthers && <Or>{t('auth.orByMail')}</Or>}
          {byMail && (
            <form
              className="hxa__stack"
              onSubmit={(e) => {
                e.preventDefault();
                void f.send();
              }}
            >
              <label className="hxa__field">
                <span className="hxa__label">{t('auth.mail')}</span>
                <input
                  className="hx-set__input"
                  type="email"
                  autoComplete="email"
                  placeholder="you@example.com"
                  value={email}
                  onChange={(e) => f.setEmail(e.target.value)}
                  aria-invalid={error ? true : undefined}
                  autoFocus={!hasOthers}
                />
              </label>
              {error && (
                <p className="hxa__err" role="alert">
                  <Icon i="circle-alert" />
                  {error}
                </p>
              )}
              <Btn type="submit" size="lg" variant={hasOthers ? undefined : 'primary'} busy={busy === 'send'}>
                {t('auth.getCode')}
              </Btn>
            </form>
          )}
          {config.auth.email.mode === 'log' && <p className="hxa__note">{t('auth.logMode')}</p>}
        </>
      )}

      {phase === 'sent' && (
        <>
          <div className="hxa__sent">
            <Icon i="mail" />
            <span>
              {t('auth.sentTo')} <b>{email}</b>
            </span>
            <Btn variant="ghost" size="sm" onClick={f.changeEmail}>
              {t('auth.change')}
            </Btn>
          </div>
          <div className="hxa__field">
            <span className="hxa__label">{t('auth.codeLabel')}</span>
            <HxOtp value={code} onChange={f.onCode} busy={busy === 'code'} invalid={codeBad} />
          </div>
          {error && (
            <p className="hxa__err" role="alert">
              <Icon i="circle-alert" />
              {error}
            </p>
          )}
          <Btn
            variant="primary"
            size="lg"
            data-block=""
            busy={busy === 'code'}
            onClick={() => code.length === 6 && void f.verify(code)}
          >
            {t('auth.continue')}
          </Btn>
          <p className="hxa__note">
            {f.secondsLeft > 0 ? (
              <>
                {t('auth.resendIn')} <b>{clock(f.secondsLeft)}</b>
              </>
            ) : (
              <button
                type="button"
                className="hxa__link"
                onClick={() => void f.send()}
                disabled={busy === 'send'}
              >
                {t('auth.resend')}
              </button>
            )}
          </p>
          {config.auth.email.mode === 'log' && <p className="hxa__note">{t('auth.logMode')}</p>}
        </>
      )}
    </AuthFrame>
  );
};
