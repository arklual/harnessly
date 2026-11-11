/**
 * Asks again and again, one ask at a time: the next one is armed `ms` after the previous one ended, so a slow answer
 * never has a second ask waiting behind it. Nothing is asked while the page is hidden; coming back asks at once.
 * The returned function stops it and aborts the ask in flight.
 */
export const startQuietPoll = (ask: (signal: AbortSignal) => Promise<unknown>, ms: number): (() => void) => {
  const abort = new AbortController();
  let timer: number | undefined;
  let asking = false;

  const arm = () => {
    window.clearTimeout(timer);
    timer = undefined;
    if (abort.signal.aborted || document.hidden || asking) return;
    timer = window.setTimeout(run, ms);
  };
  const run = () => {
    window.clearTimeout(timer);
    timer = undefined;
    if (abort.signal.aborted || document.hidden || asking) return;
    asking = true;
    void ask(abort.signal)
      .catch(() => undefined)
      .finally(() => {
        asking = false;
        arm();
      });
  };
  const onVisibility = () => {
    if (document.hidden) {
      window.clearTimeout(timer);
      timer = undefined;
      return;
    }
    run();
  };

  document.addEventListener('visibilitychange', onVisibility);
  arm();
  return () => {
    abort.abort();
    window.clearTimeout(timer);
    document.removeEventListener('visibilitychange', onVisibility);
  };
};
