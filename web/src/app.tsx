import { useCallback, useEffect, useState } from 'react';

import { auth } from './api/endpoints';
import { HttpError, onUnauthorized } from './api/http';
import { startRuntime, stopRuntime } from './stores/runtime';
import { Shell } from './ui/shell';
import { SignIn } from './ui/sign-in';
import { Splash } from './ui/splash';
import './ui/app.css';

type Phase =
  | { kind: 'checking' }
  | { kind: 'signed-out'; needsSetup: boolean }
  | { kind: 'signed-in' }
  | { kind: 'down'; message: string };

function Gate() {
  const [phase, setPhase] = useState<Phase>({ kind: 'checking' });

  const check = useCallback(async () => {
    try {
      await auth.check();
      setPhase({ kind: 'signed-in' });
    } catch (err) {
      if (err instanceof HttpError && err.status === 401) {
        try {
          const s = await auth.state();
          setPhase({ kind: 'signed-out', needsSetup: s.needsSetup });
        } catch (e) {
          setPhase({ kind: 'down', message: e instanceof Error ? e.message : String(e) });
        }
        return;
      }
      // The runtime is starting (503) or out of reach: try again shortly.
      setPhase({ kind: 'down', message: err instanceof Error ? err.message : String(err) });
    }
  }, []);

  useEffect(() => {
    // check sets the phase only after the runtime answered
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void check();
  }, [check]);

  useEffect(() => {
    if (phase.kind !== 'down') return;
    const t = setTimeout(() => void check(), 2000);
    return () => clearTimeout(t);
  }, [phase, check]);

  useEffect(() => {
    if (phase.kind !== 'signed-in') return;
    startRuntime();
    const off = onUnauthorized(() => void check());
    return () => {
      off();
      stopRuntime();
    };
  }, [phase.kind, check]);

  if (phase.kind === 'checking') return <Splash />;
  if (phase.kind === 'down') return <Splash message={`Waiting for the runtime… (${phase.message})`} />;
  if (phase.kind === 'signed-out')
    return <SignIn needsSetup={phase.needsSetup} onSignedIn={() => setPhase({ kind: 'signed-in' })} />;
  return (
    <Shell
      onSignOut={async () => {
        await auth.logout().catch(() => undefined);
        void check();
      }}
    />
  );
}

export function App() {
  return <Gate />;
}
