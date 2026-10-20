import { useCallback, useEffect, useRef } from 'react';
import { createPoller, type IPoller } from '@/lib/poller';

interface IUsePollerOptions {
  intervalMs: number | (() => number);
  maxBackoffMs: number;
  staleMs?: number;
}

/**
 * Runs `run` on the visibility-aware, single-flight schedule of `createPoller` while `key` is set.
 * A new key aborts the old run and starts over at once; `null` stops polling.
 */
const usePoller = (
  key: string | null,
  run: (signal: AbortSignal) => Promise<void>,
  options: IUsePollerOptions,
) => {
  const runRef = useRef(run);
  const optionsRef = useRef(options);
  const pollerRef = useRef<IPoller | null>(null);

  useEffect(() => {
    runRef.current = run;
    optionsRef.current = options;
  });

  useEffect(() => {
    if (key === null) return;
    const poller = createPoller({
      run: (signal) => runRef.current(signal),
      intervalMs: () => {
        const interval = optionsRef.current.intervalMs;
        return typeof interval === 'function' ? interval() : interval;
      },
      maxBackoffMs: optionsRef.current.maxBackoffMs,
      staleMs: optionsRef.current.staleMs,
    });
    pollerRef.current = poller;
    poller.start();
    return () => {
      poller.stop();
      if (pollerRef.current === poller) pollerRef.current = null;
    };
  }, [key]);

  const refresh = useCallback(() => pollerRef.current?.refresh() ?? Promise.resolve(), []);
  const poke = useCallback((delayMs?: number) => pollerRef.current?.poke(delayMs), []);
  return { refresh, poke };
};

export default usePoller;
