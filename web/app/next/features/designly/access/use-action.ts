// A pessimistic change of the access screens: pressing marks the control busy and the screen shows the new state only
// after the gateway has answered; a refusal is said in its own words. A second press while one runs does nothing.
import { useCallback, useRef, useState } from 'react';
import { t } from '@/next/i18n';

export interface IAction {
  /** Which action runs; null when none does. */
  busy: string | null;
  error: string | null;
  run: (key: string, call: () => Promise<unknown>) => Promise<boolean>;
  clear: () => void;
}

export const useAction = (): IAction => {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const running = useRef(false);

  const run = useCallback(async (key: string, call: () => Promise<unknown>) => {
    if (running.current) return false;
    running.current = true;
    setBusy(key);
    setError(null);
    try {
      await call();
      return true;
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.failure.http'));
      return false;
    } finally {
      running.current = false;
      setBusy(null);
    }
  }, []);

  const clear = useCallback(() => setError(null), []);
  return { busy, error, run, clear };
};
