// The library read again when it changes, not on a timer: the hub holds a long poll of its store
// (GET <platform>/design/store-library/changes?since=) and answers when any project of the space or a shelf has a
// newer revision. Where there is no such poll (a runtime without the hub, hub-design-home off) the page asks on the
// timer, as before; a slow timer stays behind the poll for what the store has not been told yet.
import { startQuietPoll } from '@/next/bindings/quiet-poll';
import { hubJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

/** The slow read behind the long poll: what reaches the machine but not the store still shows, later. */
export const LIBRARY_SAFETY_MS = 30_000;

/** The hub names the machine by the project itself, so the machine asked about is not sent. */
export const waitLibraryChange = async (
  _machine: MachineId,
  since: number,
  signal: AbortSignal,
): Promise<number> => {
  const answer = await hubJson<{ rev?: unknown }>(`/design/store-library/changes?since=${since}`, { signal });
  if (typeof answer.rev !== 'number') throw new Error('no revision');
  return answer.rev;
};

const visible = (signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    if (!document.hidden || signal.aborted) return resolve();
    const on = () => {
      if (document.hidden && !signal.aborted) return;
      document.removeEventListener('visibilitychange', on);
      signal.removeEventListener('abort', on);
      resolve();
    };
    document.addEventListener('visibilitychange', on);
    signal.addEventListener('abort', on);
  });

/**
 * Reads the library (read) whenever the hub says it changed, with a slow timer behind; without the hub's poll, on the
 * timer of fallbackMs. Nothing is held while the page is hidden. The returned function stops it.
 */
export const watchLibrary = (
  m: MachineId,
  read: (signal: AbortSignal) => Promise<unknown>,
  fallbackMs: number,
  wait: typeof waitLibraryChange = waitLibraryChange,
): (() => void) => {
  const abort = new AbortController();
  let stopTimer = startQuietPoll(read, LIBRARY_SAFETY_MS);
  const loop = async () => {
    let since = 0;
    let primed = false;
    while (!abort.signal.aborted) {
      await visible(abort.signal);
      if (abort.signal.aborted) return;
      let rev: number;
      try {
        rev = await wait(m, since, abort.signal);
      } catch {
        if (abort.signal.aborted) return;
        stopTimer();
        stopTimer = startQuietPoll(read, fallbackMs);
        return;
      }
      if (abort.signal.aborted) return;
      if (primed && rev > since) await read(abort.signal).catch(() => undefined);
      primed = true;
      since = Math.max(since, rev);
    }
  };
  void loop();
  return () => {
    abort.abort();
    stopTimer();
  };
};
