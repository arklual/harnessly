interface IServeStaleOptions {
  /** A value younger than this is served as it is. */
  freshMs: number;
  /** A value younger than this is served at once while a new one is fetched behind it; older ones are waited for. */
  serveStaleMs: number;
  now?: () => number;
}

/**
 * Stale-while-revalidate for a slow source: callers within `serveStaleMs` of the last value get it at once
 * and one refresh runs for all of them, so a poll through a slow link never also waits for the work behind it.
 */
export const serveStale = <T>(
  load: () => Promise<T>,
  { freshMs, serveStaleMs, now = Date.now }: IServeStaleOptions,
) => {
  let last: { at: number; value: T } | null = null;
  let running: Promise<T> | null = null;
  const refresh = (): Promise<T> => {
    running ??= load()
      .then((value) => {
        last = { at: now(), value };
        return value;
      })
      .finally(() => {
        running = null;
      });
    return running;
  };
  return (): Promise<T> => {
    const age = last ? now() - last.at : Number.POSITIVE_INFINITY;
    if (last && age < serveStaleMs) {
      if (age >= freshMs) refresh().catch(() => undefined);
      return Promise.resolve(last.value);
    }
    return refresh();
  };
};
