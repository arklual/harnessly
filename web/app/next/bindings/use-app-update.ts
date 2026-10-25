// Whether the server serves a newer build than the page running here. An open tab or an installed app keeps its page
// for days and never sees a release unless it asks: it asks when it comes back to the screen, every ten minutes while
// shown and when the network comes back (a release restarts the machines). The page is the gateway's own copy, so «new»
// means really served.
import { useCallback, useEffect, useState } from 'react';
import { entryScriptOf, entryScriptOfPage, isNewerBuild } from '@/next/lib/app-update';
import { appBasePath } from '@/platform/config';

const CHECK_EVERY_MS = 10 * 60_000;
/** Focus, visibility and «online» come in bursts: one look at the server per this long is enough. */
const MIN_GAP_MS = 60_000;

/** The entry script this page was loaded with; null when the page has none to compare (the dev server). */
const runningBuild = (): string | null =>
  entryScriptOf(
    Array.from(
      document.querySelectorAll<HTMLScriptElement>('script[type="module"][src]'),
      (s) => s.getAttribute('src') ?? '',
    ),
    document.baseURI,
  );

export const useAppUpdate = () => {
  const [available, setAvailable] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  const check = useCallback(async () => {
    const running = runningBuild();
    if (!running) return;
    const response = await fetch(`${appBasePath()}/`, {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { accept: 'text/html' },
    }).catch(() => null);
    if (!response?.ok || !(response.headers.get('content-type') ?? '').includes('text/html')) return;
    if (isNewerBuild(running, entryScriptOfPage(await response.text(), document.baseURI))) setAvailable(true);
  }, []);

  useEffect(() => {
    let last = 0;
    const maybeCheck = () => {
      if (document.visibilityState !== 'visible') return;
      const now = Date.now();
      if (now - last < MIN_GAP_MS) return;
      last = now;
      void check();
    };
    const onVisible = () => maybeCheck();
    const timer = window.setInterval(() => maybeCheck(), CHECK_EVERY_MS);
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    window.addEventListener('online', onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
      window.removeEventListener('online', onVisible);
    };
  }, [check]);

  return {
    available: available && !dismissed,
    reload: () => window.location.reload(),
    dismiss: () => setDismissed(true),
  };
};
