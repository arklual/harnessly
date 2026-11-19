import { useCallback, useEffect, useRef, useState } from 'react';

import { useEnv } from './env';

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
