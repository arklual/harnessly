// How fast Sinopia's home is for a person: the time from opening it to every card drawn, and the largest
// contentful paint. Each number goes in one small POST to the server (the hub counts it in a histogram; a runtime with
// no hub writes it to its journal). A failed report is dropped: a number lost is not worth a line on screen.
import { useEffect, useRef } from 'react';
import { clusterApi } from '@/next/core/net/machine';

/** The closed set the server takes; it drops any other name. */
export const WEB_VITAL = { homeFull: 'sinopia_home_full', lcp: 'lcp' } as const;
export type TWebVital = (typeof WEB_VITAL)[keyof typeof WEB_VITAL];

export const reportWebVital = (name: TWebVital, seconds: number): void => {
  if (!Number.isFinite(seconds) || seconds <= 0) return;
  try {
    void fetch(clusterApi('/web-vitals'), {
      method: 'POST',
      credentials: 'same-origin',
      keepalive: true,
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ name, seconds: Math.round(seconds * 1000) / 1000 }),
    }).catch(() => undefined);
  } catch {
    // no fetch (a test, an old browser): nothing to report with
  }
};

/** The last largest-contentful-paint entry the browser has made, in seconds; null where it does not measure it. */
const lcpSeconds = (): number | null => {
  try {
    const entries = performance.getEntriesByType?.('largest-contentful-paint') ?? [];
    const last = entries[entries.length - 1];
    return last ? last.startTime / 1000 : null;
  } catch {
    return null;
  }
};

let lcpValue: number | null = null;
let lcpWatched = false;
const watchLcp = () => {
  if (lcpWatched || typeof PerformanceObserver === 'undefined') return;
  lcpWatched = true;
  try {
    new PerformanceObserver((list) => {
      const entries = list.getEntries();
      const last = entries[entries.length - 1];
      if (last) lcpValue = last.startTime / 1000;
    }).observe({ type: 'largest-contentful-paint', buffered: true });
  } catch {
    // the browser does not know the type
  }
};

/** One page load reports once: the home is opened again many times, only the first is a cold one. */
let reported = false;

/**
 * Reports the time from the home's mount to its cards on screen, and the LCP with it. `ready`: the cards are there.
 * A home that mounts with its cards already there (the last library kept in memory) is not a wait and is not counted.
 */
export const useHomeVitals = (ready: boolean): void => {
  const mounted = useRef<{ at: number; cold: boolean }>({ at: 0, cold: !ready });
  useEffect(() => {
    mounted.current.at = performance.now();
    watchLcp();
  }, []);
  useEffect(() => {
    if (!ready || reported || !mounted.current.cold) return;
    // Two frames after the cards are in the tree: they are drawn, not just rendered.
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => {
        if (reported) return;
        reported = true;
        reportWebVital(WEB_VITAL.homeFull, (performance.now() - mounted.current.at) / 1000);
        const lcp = lcpValue ?? lcpSeconds();
        if (lcp !== null) reportWebVital(WEB_VITAL.lcp, lcp);
      });
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [ready]);
};

/** Only for tests: a page load that has not reported yet. */
export const resetWebVitals = () => {
  reported = false;
};
