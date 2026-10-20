// The runtime keeps one password in its config. A fresh machine has none: the first visitor sets it (the runtime
// listens on loopback by default, so that is the person at the machine), and then signs in with it. A machine started
// with an initial password (INIT_PASSWORD) asks for that first, so nobody else can claim it, and then offers the setup.
import { useState, type FormEvent } from 'react';
import HlMark from '@/components/agents/hl-mark';
import { auth } from '@/runtime/endpoints';
import { appConfig } from '@/platform/config';
import './sign-in.css';
import { t } from '@/lib/i18n';

type TMode = 'signin' | 'init' | 'setup';

export default function SignIn({
  needsSetup,
  requiresAuth = false,
  onSignedIn,
}: {
  needsSetup: boolean;
  /** The machine has no password yet but an initial one stands guard: sign in with it before the setup. */
  requiresAuth?: boolean;
  onSignedIn: () => void;
}) {
  const [mode, setMode] = useState<TMode>(needsSetup ? (requiresAuth ? 'init' : 'setup') : 'signin');
  const setup = mode === 'setup';
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (setup) {
      if (password.length < 4) return setError(t('auth.signIn.passwordTooShort'));
      if (password !== confirm) return setError(t('auth.signIn.passwordsDontMatch'));
    }
    setBusy(true);
    try {
      if (setup) await auth.setup(password);
      await auth.login(password);
      if (mode === 'init') {
        setPassword('');
        setMode('setup');
      } else onSignedIn();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="hl-signin">
      <form className="hl-signin-card" data-testid={`${mode}-form`} onSubmit={submit}>
        <HlMark className="hl-signin-mark" />
        <h1>
          {mode === 'init'
            ? t('auth.signIn.initTitle')
            : setup
              ? t('auth.signIn.setupTitle')
              : t('auth.signIn.signInTitle', { product: appConfig().productName })}
        </h1>
        <p>
          {mode === 'init'
            ? t('auth.signIn.initHint')
            : setup
              ? t('auth.signIn.setupHint')
              : t('auth.signIn.signInHint')}
        </p>
        <label>
          <span>{t('auth.signIn.password')}</span>
          <input
            type="password"
            name="password"
            autoComplete={setup ? 'new-password' : 'current-password'}
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {setup && (
          <label>
            <span>{t('auth.signIn.passwordAgain')}</span>
            <input
              type="password"
              name="confirm"
              autoComplete="new-password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </label>
        )}
        {error && (
          <p className="hl-signin-error" role="alert">
            {error}
          </p>
        )}
        <button type="submit" className="hl-btn" disabled={!password || busy}>
          {busy
            ? t('auth.signIn.checking')
            : setup
              ? t('auth.signIn.saveAndSignIn')
              : t('auth.signIn.signIn')}
        </button>
      </form>
    </div>
  );
}
