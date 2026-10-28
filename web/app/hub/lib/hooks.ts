import { useCallback, useEffect, useReducer, useRef, useState } from 'react';

import { type ApiError, eventsUrl, isApiError } from './api';
import { useEnv } from './env';
import { EVENT_TYPES, initialLive, parseSpaceEvent, reduceLive, type LiveState } from './live';

/** The time, ticking every `ms` while `active`. */
export const useNow = (ms = 1000, active = true): number => {
  const env = useEnv();
  const [now, setNow] = useState(() => env.now());
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setNow(env.now()), ms);
    return () => window.clearInterval(timer);
  }, [env, ms, active]);
  return now;
};

/** `copy(text)` and whether it was copied a moment ago (the button shows a check for 2 s). */
export const useCopy = (): { copied: boolean; copy: (text: string) => void } => {
  const env = useEnv();
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);
  const copy = useCallback(
    (text: string) => {
      void env.copyText(text).then((ok) => {
        if (!ok) return;
        setCopied(true);
        window.clearTimeout(timer.current);
        timer.current = window.setTimeout(() => setCopied(false), 2000);
      });
    },
    [env],
  );
  return { copied, copy };
};

export interface Live {
  state: LiveState;
  /** Set when the machines could not be read; the stream keeps trying on its own. */
  error: ApiError | null;
  reload: () => void;
  /** Puts a job or login that came from a POST answer in the state before its first event arrives. */
  push: (action: Parameters<typeof reduceLive>[1]) => void;
}

/**
 * A space's live data: fetches the machines, then follows the SSE stream. The stream has no replay, so every
 * (re)connect fetches again.
 */
export const useSpaceLive = (spaceId: string): Live => {
  const env = useEnv();
  const [state, dispatch] = useReducer(reduceLive, initialLive);
  const [error, setError] = useState<ApiError | null>(null);

  const load = useCallback(async () => {
    try {
      const { machines } = await env.api.machines.list(spaceId);
      dispatch({ type: 'loaded', machines });
      setError(null);
    } catch (e) {
      if (isApiError(e)) setError(e);
    }
  }, [env, spaceId]);

  useEffect(() => {
    let alive = true;
    env.api.machines.list(spaceId).then(
      ({ machines }) => {
        if (!alive) return;
        dispatch({ type: 'loaded', machines });
        setError(null);
      },
      (e: unknown) => {
        if (alive && isApiError(e)) setError(e);
      },
    );
    const es = env.eventSource(eventsUrl(spaceId));
    es.onopen = () => void load();
    for (const type of EVENT_TYPES) {
      es.addEventListener(type, (m) => {
        const event = parseSpaceEvent(type, m.data);
        if (event) dispatch({ type: 'event', event });
      });
    }
    return () => {
      alive = false;
      es.close();
    };
  }, [env, spaceId, load]);

  return { state, error, reload: () => void load(), push: dispatch };
};
