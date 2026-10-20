import { isPageHidden, subscribeFrameVisibility } from '@/lib/frame-visibility';

/**
 * Background refresh loop for data a page keeps on screen, tuned for a slow or lossy link:
 *
 * - one request at a time: calls made while a run is in flight share a single trailing run,
 *   so a slow answer never piles up identical requests behind it;
 * - the next run is timed from the end of the previous one, not from its start;
 * - nothing runs while the tab is hidden; a tab that comes back with stale data refreshes at once;
 * - failures back off exponentially up to `maxBackoffMs`;
 * - `poke` turns a change signal (a WebSocket message) into one coalesced run, so the timer itself can stay slow.
 *
 * `run` reports failure by throwing and is responsible for surfacing the error to the UI;
 * the poller only counts failures to space out retries.
 */

/**
 * Why a run happens: `signal` when something is known to have changed (`poke`, `refresh`, a poke while hidden),
 * so a server-side cache must not answer it; `schedule` for the timer, the first load and a tab coming back.
 */
export type TPollCause = 'schedule' | 'signal';

const strongerCause = (a: TPollCause | null, b: TPollCause): TPollCause => (a === 'signal' ? a : b);

export interface IVisibilitySource {
  isHidden: () => boolean;
  /** Fires when the tab is shown or hidden and when the browser comes back online. */
  subscribe: (listener: () => void) => () => void;
}

export const documentVisibility: IVisibilitySource = {
  isHidden: isPageHidden,
  subscribe: (listener) => {
    const unsubscribeFrame = subscribeFrameVisibility(listener);
    if (typeof document === 'undefined') return unsubscribeFrame;
    document.addEventListener('visibilitychange', listener);
    window.addEventListener('online', listener);
    return () => {
      document.removeEventListener('visibilitychange', listener);
      window.removeEventListener('online', listener);
      unsubscribeFrame();
    };
  },
};

export interface IPollerOptions {
  run: (signal: AbortSignal, cause: TPollCause) => Promise<void>;
  /** Pause between the end of one successful run and the start of the next; a function is read before every pause. */
  intervalMs: number | (() => number);
  /** Longest pause after consecutive failures. */
  maxBackoffMs: number;
  /** Data older than this is refreshed as soon as the tab is visible again. */
  staleMs?: number;
  visibility?: IVisibilitySource;
}

export interface IPoller {
  start: () => void;
  stop: () => void;
  /** Resolves after a run that started after this call — joining the trailing slot if one is in flight. */
  refresh: () => Promise<void>;
  /** Something changed on the server: run once after `delayMs`, merged with any other pokes in that window. */
  poke: (delayMs?: number) => void;
}

export const POLLER_DEFAULTS = {
  staleMs: 5_000,
  pokeDelayMs: 1_000,
  /** First retry after a failure never waits longer than this, however slow the regular interval is. */
  firstRetryMs: 5_000,
} as const;

/** Pause before the next run: the regular interval, or an exponential backoff after `failures` failed runs in a row. */
export const nextPollDelay = (intervalMs: number, failures: number, maxBackoffMs: number): number => {
  if (failures === 0) return intervalMs;
  const base = Math.min(intervalMs, POLLER_DEFAULTS.firstRetryMs);
  return Math.min(maxBackoffMs, base * 2 ** (failures - 1));
};

export const createPoller = (options: IPollerOptions): IPoller => {
  const visibility = options.visibility ?? documentVisibility;
  const staleMs = options.staleMs ?? POLLER_DEFAULTS.staleMs;
  let active = false;
  let inFlight: Promise<void> | null = null;
  let trailing: Promise<void> | null = null;
  let trailingCause: TPollCause | null = null;
  let controller: AbortController | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;
  let timerDueAt = 0;
  let timerCause: TPollCause = 'schedule';
  let lastRunAt: number | null = null;
  let failures = 0;
  /** A poke arrived while the tab was hidden. */
  let dirty = false;
  let unsubscribe: (() => void) | null = null;

  const interval = () =>
    typeof options.intervalMs === 'function' ? options.intervalMs() : options.intervalMs;

  const clearTimer = () => {
    if (timer === null) return;
    clearTimeout(timer);
    timer = null;
  };

  const setTimer = (ms: number, cause: TPollCause) => {
    clearTimer();
    timerDueAt = Date.now() + ms;
    timerCause = cause;
    timer = setTimeout(() => {
      timer = null;
      void execute(timerCause);
    }, ms);
  };

  const schedule = () => {
    if (!active || inFlight || visibility.isHidden()) return;
    setTimer(nextPollDelay(interval(), failures, options.maxBackoffMs), 'schedule');
  };

  const execute = (cause: TPollCause): Promise<void> => {
    if (inFlight) {
      trailingCause = strongerCause(trailingCause, cause);
      trailing ??= inFlight.then(() => {
        const next = trailingCause ?? 'schedule';
        trailing = null;
        trailingCause = null;
        // Stopped meanwhile: nobody is left to show the result.
        return active ? execute(next) : undefined;
      });
      return trailing;
    }
    clearTimer();
    dirty = false;
    const current = new AbortController();
    controller = current;
    const attempt = async () => {
      try {
        await options.run(current.signal, cause);
        failures = 0;
      } catch {
        if (!current.signal.aborted) failures += 1;
      }
    };
    inFlight = attempt().finally(() => {
      lastRunAt = Date.now();
      inFlight = null;
      if (controller === current) controller = null;
      if (!trailing) schedule();
    });
    return inFlight;
  };

  const resume = () => {
    if (!active) return;
    if (visibility.isHidden()) {
      clearTimer();
      return;
    }
    if (inFlight) return;
    const stale = lastRunAt === null || Date.now() - lastRunAt >= staleMs;
    if (dirty) {
      void execute('signal');
      return;
    }
    if (failures > 0 || stale) {
      void execute('schedule');
      return;
    }
    if (timer === null) schedule();
  };

  const start = () => {
    if (active) return;
    active = true;
    unsubscribe = visibility.subscribe(resume);
    // A run still in flight was aborted by stop(): queue a fresh one behind it.
    // The very first load happens even in a background tab: the page is useless without it.
    if (inFlight || lastRunAt === null) {
      void execute('schedule');
      return;
    }
    resume();
  };

  const stop = () => {
    if (!active) return;
    active = false;
    unsubscribe?.();
    unsubscribe = null;
    clearTimer();
    controller?.abort();
  };

  const poke = (delayMs: number = POLLER_DEFAULTS.pokeDelayMs) => {
    if (!active) return;
    if (visibility.isHidden()) {
      dirty = true;
      return;
    }
    if (inFlight) {
      void execute('signal');
      return;
    }
    if (timer !== null && timerDueAt <= Date.now() + delayMs) {
      timerCause = 'signal';
      return;
    }
    setTimer(delayMs, 'signal');
  };

  return { start, stop, refresh: () => execute('signal'), poke };
};
