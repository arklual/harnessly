// The runtime keeps one password in its config. A fresh machine has none: the first visitor sets it (the runtime
// listens on loopback by default, so that is the person at the machine), and then signs in with it.
import { useState, type FormEvent } from 'react';
import HlMark from '@/components/agents/hl-mark';
import { auth } from '@/runtime/endpoints';
import { appConfig } from '@/platform/config';
import './sign-in.css';
import { t } from '@/lib/i18n';

export default function SignIn({ needsSetup, onSignedIn }: { needsSetup: boolean; onSignedIn: () => void }) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    if (needsSetup) {
      if (password.length < 4) return setError(t('auth.signIn.passwordTooShort'));
      if (password !== confirm) return setError(t('auth.signIn.passwordsDontMatch'));
    }
    setBusy(true);
    try {
      if (needsSetup) await auth.setup(password);
      await auth.login(password);
      onSignedIn();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="hl-signin">
      <form
        className="hl-signin-card"
        data-testid={needsSetup ? 'setup-form' : 'signin-form'}
        onSubmit={submit}
      >
        <HlMark className="hl-signin-mark" />
        <h1>
          {needsSetup
            ? t('auth.signIn.setupTitle')
            : t('auth.signIn.signInTitle', { product: appConfig().productName })}
        </h1>
        <p>{needsSetup ? t('auth.signIn.setupHint') : t('auth.signIn.signInHint')}</p>
        <label>
          <span>{t('auth.signIn.password')}</span>
          <input
            type="password"
            name="password"
            autoComplete={needsSetup ? 'new-password' : 'current-password'}
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {needsSetup && (
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
            : needsSetup
              ? t('auth.signIn.saveAndSignIn')
              : t('auth.signIn.signIn')}
        </button>
      </form>
    </div>
  );
}
