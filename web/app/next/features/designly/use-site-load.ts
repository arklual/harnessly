// The clock of an opening design: whether the load has gone on longer than usual, and whether it has gone on for too
// long to wait for. It runs from the moment the page is asked for (not from the frame's own start), and starts over
// with every new ask (another page, a reload, «Повторить»).
import { useEffect, useState } from 'react';
import { SLOW_MS, STALL_MS } from '@/next/features/designly/site-load';

export const useSiteLoad = (key: string, pending: boolean) => {
  const [slowFor, setSlowFor] = useState<string | null>(null);
  const [stalledFor, setStalledFor] = useState<string | null>(null);
  useEffect(() => {
    if (!pending) return;
    const slow = window.setTimeout(() => setSlowFor(key), SLOW_MS);
    const stall = window.setTimeout(() => setStalledFor(key), STALL_MS);
    return () => {
      window.clearTimeout(slow);
      window.clearTimeout(stall);
    };
  }, [key, pending]);
  return { slow: pending && slowFor === key, stalled: pending && stalledFor === key };
};
