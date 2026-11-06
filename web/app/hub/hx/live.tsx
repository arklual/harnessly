// The lively hub (feature `ui.hx.live`): one switch the screens read, and the number that counts up when it changes.
import { createContext, useContext, useEffect, useRef, useState } from 'react';

import { DUR_MS, tweenText } from './live-text';

/** True inside the shell of an organization that has `ui.hx.live`; every lively effect asks it first. */
export const LiveCtx = createContext(false);
export const useLive = (): boolean => useContext(LiveCtx);

const reduced = (): boolean => {
  try {
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  } catch {
    return false;
  }
};

/** A formatted number that counts from the previous one to the new one (the first draw is also a count from zero). */
export const Count = ({ text }: { text: string }) => {
  const live = useLive();
  const [shown, setShown] = useState(text);
  const from = useRef<string | null>(null);
  const calm = !live || reduced();
  useEffect(() => {
    if (calm) {
      from.current = text;
      return;
    }
    const start = from.current ?? tweenText(text, text, 0);
    const t0 = performance.now();
    let raf = 0;
    const step = (now: number) => {
      const k = Math.min(1, (now - t0) / DUR_MS);
      setShown(tweenText(start, text, 1 - (1 - k) ** 3));
      if (k < 1) raf = requestAnimationFrame(step);
      else from.current = text;
    };
    raf = requestAnimationFrame(step);
    return () => {
      cancelAnimationFrame(raf);
      from.current = text;
    };
  }, [text, calm]);
  return <>{calm ? text : shown}</>;
};
