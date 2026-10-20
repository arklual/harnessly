/**
 * The server holds back push notifications for a device it saw looking at the app within the last minute
 * (`VISIBILITY_TTL` in push-subscriptions). The client only has to say when that changes and refresh
 * a "still looking" claim before it expires — not echo every focus and blur.
 */
export const VISIBILITY_REPORT = {
  /** How often an app in focus re-confirms itself. */
  pingMs: 30_000,
  /** A repeated "visible" is sent only when the previous one is at least this old; the worst gap stays under the server's minute. */
  refreshAfterMs: 20_000,
  /** Focus hops (window → iframe → window) settle before anything is sent. */
  settleMs: 400,
} as const;

/** Returns `report(visible)` that forwards to `send` only what the server does not know yet. */
export const createVisibilityReporter = (send: (visible: boolean) => void) => {
  let last: { visible: boolean; at: number } | null = null;
  return (visible: boolean) => {
    const now = Date.now();
    if (last && last.visible === visible && (!visible || now - last.at < VISIBILITY_REPORT.refreshAfterMs))
      return;
    last = { visible, at: now };
    send(visible);
  };
};

export interface IVisibilityHost {
  window: Pick<EventTarget, 'addEventListener' | 'removeEventListener'>;
  document: Pick<EventTarget, 'addEventListener' | 'removeEventListener'> & {
    readonly visibilityState: DocumentVisibilityState;
    hasFocus: () => boolean;
  };
}

/**
 * Tells `report` whether the user is looking at the app until the returned stop function runs.
 * Leaving the page (hidden, pagehide) is reported at once: a mobile browser freezes timers the moment
 * the page goes to the background, and a missed "hidden" holds back push notifications for up to a minute.
 * Only focus hops while the page stays on screen (window → iframe → window) wait for `settleMs`.
 */
export const trackVisibility = (report: (visible: boolean) => void, host: IVisibilityHost): (() => void) => {
  const { window: win, document: doc } = host;
  const looking = () => doc.visibilityState === 'visible' && doc.hasFocus();
  let settleTimer: ReturnType<typeof setTimeout> | null = null;
  const cancelSettle = () => {
    if (settleTimer === null) return;
    clearTimeout(settleTimer);
    settleTimer = null;
  };
  const reportLeft = () => {
    cancelSettle();
    report(false);
  };
  const reportSettled = () => {
    cancelSettle();
    if (doc.visibilityState === 'hidden') {
      report(false);
      return;
    }
    settleTimer = setTimeout(() => {
      settleTimer = null;
      report(looking());
    }, VISIBILITY_REPORT.settleMs);
  };

  report(looking());
  win.addEventListener('focus', reportSettled);
  win.addEventListener('blur', reportSettled);
  win.addEventListener('pagehide', reportLeft);
  win.addEventListener('pageshow', reportSettled);
  doc.addEventListener('visibilitychange', reportSettled);
  const pingTimer = setInterval(() => {
    if (looking()) report(true);
  }, VISIBILITY_REPORT.pingMs);

  return () => {
    win.removeEventListener('focus', reportSettled);
    win.removeEventListener('blur', reportSettled);
    win.removeEventListener('pagehide', reportLeft);
    win.removeEventListener('pageshow', reportSettled);
    doc.removeEventListener('visibilitychange', reportSettled);
    cancelSettle();
    clearInterval(pingTimer);
  };
};
