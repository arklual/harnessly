import { useCallback, useEffect, useState } from 'react';
import { APP_BASE_PATH } from '@/lib/app-path';
import { entryScriptOf, entryScriptOfPage, isNewerBuild } from '@/lib/app-update';
import { PUSH_MESSAGE_EVENT } from '@/hooks/use-web-push';
import type { IPushMessage } from '@/lib/push-message';

const CHECK_EVERY_MS = 10 * 60_000;
/** Focus, visibility and «online» come in bursts: one look at the server per this long is enough. */
const MIN_GAP_MS = 60_000;

/** The entry script this page was loaded with; null when the page has none to compare (the dev server). */
const runningBuild = (): string | null =>
  typeof document === 'undefined'
    ? null
    : entryScriptOf(
        Array.from(
          document.querySelectorAll<HTMLScriptElement>('script[type="module"][src]'),
          (s) => s.getAttribute('src') ?? '',
        ),
        document.baseURI,
      );

/**
 * Whether the server has a newer build than the page running here. An installed app keeps its page for
 * days: it never sees a release unless it asks. It asks when it comes back to the screen, every ten minutes while
 * shown, and at once when a release push arrives. The page is the gateway's own copy, so «new» means really served.
 */
const useAppUpdate = () => {
  const [available, setAvailable] = useState(false);
  const [dismissed, setDismissed] = useState(false);

  const check = useCallback(async () => {
    const running = runningBuild();
    if (!running) return;
    // The page itself, not a machine-scoped path: it is what the gateway serves to a reload.
    const response = await fetch(`${APP_BASE_PATH}/`, {
      cache: 'no-store',
      credentials: 'same-origin',
      headers: { accept: 'text/html' },
    }).catch(() => null);
    if (!response?.ok || !(response.headers.get('content-type') ?? '').includes('text/html')) return;
    if (isNewerBuild(running, entryScriptOfPage(await response.text(), document.baseURI))) setAvailable(true);
  }, []);

  useEffect(() => {
    let last = 0;
    const maybeCheck = (force = false) => {
      if (document.visibilityState !== 'visible') return;
      const now = Date.now();
      if (!force && now - last < MIN_GAP_MS) return;
      last = now;
      void check();
    };
    const onVisible = () => maybeCheck();
    const onPush = (event: Event) => {
      if ((event as CustomEvent<IPushMessage>).detail?.kind === 'release') maybeCheck(true);
    };
    const timer = window.setInterval(() => maybeCheck(), CHECK_EVERY_MS);
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('focus', onVisible);
    window.addEventListener('online', onVisible);
    window.addEventListener(PUSH_MESSAGE_EVENT, onPush);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('focus', onVisible);
      window.removeEventListener('online', onVisible);
      window.removeEventListener(PUSH_MESSAGE_EVENT, onPush);
    };
  }, [check]);

  return {
    available: available && !dismissed,
    reload: () => window.location.reload(),
    dismiss: () => setDismissed(true),
  };
};

export default useAppUpdate;
