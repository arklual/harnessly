// A pessimistic action of a control (spec §3, §12): pressing it turns the control busy at once, after 3 s without an
// answer it says it takes longer, after 15 s it gives up with an error and a Retry, so nothing spins forever. The result
// comes from the caller's promise; a late answer after the give-up still settles it.
import { useCallback, useEffect, useRef, useState } from 'react';

export const PENDING_SLOW_MS = 3000;
export const PENDING_TIMEOUT_MS = 15000;

export const PENDING = { idle: 'idle', busy: 'busy', slow: 'slow', failed: 'failed' } as const;
export type TPendingPhase = (typeof PENDING)[keyof typeof PENDING];

export interface IPending<K extends string> {
  phase: TPendingPhase;
  /** Which of the control's actions runs or failed last; null when idle. */
  key: K | null;
  /** Runs one action; a second press while one runs does nothing. */
  run: (key: K, action: () => Promise<unknown>) => void;
  /** Runs the failed action again. */
  retry: () => void;
  /** Forgets a failure. */
  reset: () => void;
}

const usePending = <K extends string = string>(): IPending<K> => {
  const [state, setState] = useState<{ phase: TPendingPhase; key: K | null }>({
    phase: PENDING.idle,
    key: null,
  });
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);
  const attempt = useRef(0);
  const last = useRef<{ key: K; action: () => Promise<unknown> } | null>(null);
  const running = useRef(false);

  const clear = () => {
    timers.current.forEach(clearTimeout);
    timers.current = [];
  };

  useEffect(() => clear, []);

  const run = useCallback((key: K, action: () => Promise<unknown>) => {
    if (running.current) return;
    running.current = true;
    last.current = { key, action };
    const mine = ++attempt.current;
    setState({ phase: PENDING.busy, key });
    clear();
    timers.current.push(
      setTimeout(() => {
        if (attempt.current === mine) setState({ phase: PENDING.slow, key });
      }, PENDING_SLOW_MS),
      setTimeout(() => {
        if (attempt.current !== mine) return;
        running.current = false;
        setState({ phase: PENDING.failed, key });
      }, PENDING_TIMEOUT_MS),
    );
    let settled: Promise<unknown>;
    try {
      settled = Promise.resolve(action());
    } catch (error) {
      settled = Promise.reject(error);
    }
    settled.then(
      () => {
        if (attempt.current !== mine) return;
        clear();
        running.current = false;
        setState({ phase: PENDING.idle, key: null });
      },
      () => {
        if (attempt.current !== mine) return;
        clear();
        running.current = false;
        setState({ phase: PENDING.failed, key });
      },
    );
  }, []);

  const retry = useCallback(() => {
    if (last.current) run(last.current.key, last.current.action);
  }, [run]);

  const reset = useCallback(() => {
    attempt.current += 1;
    clear();
    running.current = false;
    setState({ phase: PENDING.idle, key: null });
  }, []);

  return { ...state, run, retry, reset };
};

export default usePending;

/** The control is waiting for its answer. */
export const isBusy = (phase: TPendingPhase): boolean => phase === PENDING.busy || phase === PENDING.slow;
