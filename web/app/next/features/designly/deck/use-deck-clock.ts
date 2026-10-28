// A running clock for the show: whole seconds since it started, not counting the time it was held. The tick is 250 ms,
// the time itself comes from the performance clock, so a slow tick never loses a second.
import { useEffect, useState } from 'react';
import { Stopwatch } from '@/next/features/designly/deck/clock';

const TICK_MS = 250;

/** Seconds run; `running` false holds the clock where it is. */
export const useDeckClock = (running: boolean): number => {
  const [watch] = useState(() => {
    const w = new Stopwatch();
    w.start(performance.now());
    return w;
  });
  const [seconds, setSeconds] = useState(0);
  useEffect(() => {
    if (!running) return;
    watch.resume(performance.now());
    const id = setInterval(() => setSeconds(Math.floor(watch.elapsed(performance.now()) / 1000)), TICK_MS);
    return () => {
      clearInterval(id);
      watch.pause(performance.now());
    };
  }, [running, watch]);
  return seconds;
};
