import { Button, Stack, TextField } from '@kit';
import { type FormEvent, useState } from 'react';

import { auth } from '../api/endpoints';
import { Logo } from './logo';

/**
 * The runtime keeps one password in its config. A fresh machine has none: the first visitor sets it (the runtime
 * listens on loopback by default, so that is the person at the machine), and then signs in with it.
 */
export function SignIn({ needsSetup, onSignedIn }: { needsSetup: boolean; onSignedIn: () => void }) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    if (needsSetup) {
      if (password.length < 4) return setError('Use at least 4 characters.');
      if (password !== confirm) return setError('The passwords differ.');
    }
    setBusy(true);
    try {
      if (needsSetup) await auth.setup(password);
      await auth.login(password);
      onSignedIn();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="sign-in">
      <form className="sign-in__card" onSubmit={submit}>
        <Stack gap={6}>
          <Stack gap={2} align="start">
            <Logo size={36} />
            <h1 className="sign-in__title">{needsSetup ? 'Set up this machine' : 'Sign in'}</h1>
            <p className="sign-in__text">
              {needsSetup
                ? 'Choose the password that protects terminals and agents on this machine.'
                : 'Enter the password of this machine.'}
            </p>
          </Stack>
          <Stack gap={4}>
            <TextField
              label="Password"
              type="password"
              name="password"
              autoComplete={needsSetup ? 'new-password' : 'current-password'}
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              error={!needsSetup ? error : undefined}
            />
            {needsSetup && (
              <TextField
                label="Repeat the password"
                type="password"
                name="confirm"
                autoComplete="new-password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                error={error}
              />
            )}
          </Stack>
          <Button type="submit" appearance="primary" size="m" block loading={busy} disabled={!password}>
            {needsSetup ? 'Set password and continue' : 'Sign in'}
          </Button>
        </Stack>
      </form>
    </div>
  );
}
