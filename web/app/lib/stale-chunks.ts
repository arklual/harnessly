// A page opened before the runtime was updated asks for chunks of the old build, which are no longer there: Safari says
// «Importing a module script failed», and a screen or the whole interface did not open. Such a failure
// reloads the page once, which brings the new build; a second one within a minute is let through to the screen's own
// «Повторить», so a chunk that is missing for another reason never makes the page reload over and over.

const KEY = 'hl-stale-chunk-reload';
/** A reload this soon after the last one is not done again. */
export const STALE_RELOAD_GAP_MS = 60_000;

interface IStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** Whether to reload now, writing down that it is done; false when it was done a moment ago or nothing can be kept. */
export const reloadOnce = (store: IStore | null, now: number): boolean => {
  if (store === null) return false;
  try {
    const kept = store.getItem(KEY);
    const last = kept === null ? Number.NaN : Number(kept);
    if (Number.isFinite(last) && now - last < STALE_RELOAD_GAP_MS) return false;
    store.setItem(KEY, String(now));
    return true;
  } catch {
    return false;
  }
};

const sessionStore = (): IStore | null => {
  try {
    return window.sessionStorage;
  } catch {
    return null;
  }
};

/** Listens for the chunks of an old build that fail to load (Vite's `vite:preloadError`) and reloads the page once. */
export const reloadOnStaleChunks = (): void => {
  if (typeof window === 'undefined') return;
  window.addEventListener('vite:preloadError', (event) => {
    if (!navigator.onLine) return;
    if (!reloadOnce(sessionStore(), Date.now())) return;
    event.preventDefault();
    window.location.reload();
  });
};
