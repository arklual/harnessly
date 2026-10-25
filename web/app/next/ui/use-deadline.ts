// A wait that does not go on for ever: `expired` turns true when `loading` has held for `ms`, and the screen
// then says it did not load with a «Повторить» instead of a skeleton that spins for ever. `restart` waits again.
import { useCallback, useEffect, useState } from 'react';

export const LOAD_DEADLINE_MS = 15_000;

export interface IDeadline {
  expired: boolean;
  /** Starts the wait over (the retry was pressed). */
  restart: () => void;
}

export const useDeadline = (loading: boolean, ms = LOAD_DEADLINE_MS): IDeadline => {
  const [expired, setExpired] = useState(false);
  const [round, setRound] = useState(0);
  useEffect(() => {
    if (!loading) return;
    const timer = setTimeout(() => setExpired(true), ms);
    return () => {
      clearTimeout(timer);
      setExpired(false);
    };
  }, [loading, ms, round]);
  const restart = useCallback(() => setRound((n) => n + 1), []);
  return { expired: loading && expired, restart };
};
