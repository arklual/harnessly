import { useEffect, useRef, type RefObject } from 'react';
import { journal } from '@/next/core/journal';

// A board's frame that got something else than the board (the hub's frameSafe page, a refused slot, the page that asks
// for a reload past the slot, or a refusal the browser shows by itself and nobody can read) opens anew by itself: a
// fresh address takes a fresh key of the board's files and a hub that is back. The waits grow, so a machine that stays
// away is not asked every second.

const FIRST_MS = 2000;
const MOST_MS = 30000;

/** A frame that loaded and whose board never said «lib-ready» in this long holds something else than the board. */
export const FRAME_SILENT_MS = 10000;
/** At most this many new openings in a row for a silent frame: a board without the runtime's script stays as it is. */
export const FRAME_SILENT_TRIES = 4;

/** How long to wait before the attempt-th new opening (0 is the first). */
export const frameRetryDelay = (attempt: number): number =>
  Math.min(FIRST_MS * 2 ** Math.max(0, attempt), MOST_MS);

/**
 * What the frame's page says of itself: refused (hlViewError, kind «refused»: the hub's frame page, a refused slot) or
 * reload (the hub's page for a navigation past the slot, hl-preview-reload); null for anything else.
 */
export const frameFailure = (data: unknown): 'refused' | 'reload' | null => {
  if (typeof data !== 'object' || data === null) return null;
  if ((data as { type?: unknown }).type === 'hl-preview-reload') return 'reload';
  if (!('hlViewError' in data)) return null;
  const said = (data as { hlViewError: unknown }).hlViewError;
  return typeof said === 'object' && said !== null && (said as { kind?: unknown }).kind === 'refused'
    ? 'refused'
    : null;
};

export interface IFrameRetryOptions {
  frameRef: RefObject<HTMLIFrameElement | null>;
  /** The frame's address; null while there is none. */
  src: string | null;
  /** The frame's address and its reload: a new one is a new load. */
  frameKey: string;
  /** The key of the load the frame finished, and the one whose board said «lib-ready». */
  loadedFor: string | null;
  libReadyFor: string | null;
  /** Opens the frame anew with a fresh address (the view's reload). */
  onRetry: () => void;
}

/**
 * Something else than the board in the frame (a break of the link, a sign-in, a key of the board's files that ran
 * out, the hub's page for a navigation past the slot, or the browser's own «refused to connect», which the page can
 * not read): the frame opens anew by itself, with growing waits, until the board says it is there. Each time is a line
 * of the machine's journal (source sinopia), so a failure the person sees is never silent there.
 */
export const useFrameRetry = ({
  frameRef,
  src,
  frameKey,
  loadedFor,
  libReadyFor,
  onRetry,
}: IFrameRetryOptions) => {
  const refusals = useRef(0);
  const latestRetry = useRef(onRetry);
  const latestSrc = useRef(src);
  const retryTimer = useRef<number | undefined>(undefined);
  useEffect(() => {
    latestRetry.current = onRetry;
    latestSrc.current = src;
  }, [onRetry, src]);
  useEffect(() => {
    if (libReadyFor === frameKey) refusals.current = 0;
  }, [libReadyFor, frameKey]);
  const retryFrame = useRef((ev: string, text: string) => {
    journal({ level: 'warn', src: 'sinopia', ev, text, url: latestSrc.current ?? undefined });
    window.clearTimeout(retryTimer.current);
    retryTimer.current = window.setTimeout(() => latestRetry.current(), frameRetryDelay(refusals.current));
    refusals.current += 1;
  });
  useEffect(() => () => window.clearTimeout(retryTimer.current), []);
  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const frame = frameRef.current;
      if (frame === null || event.source !== frame.contentWindow) return;
      const failure = frameFailure(event.data);
      if (failure === null) return;
      const said = (event.data as { hlViewError?: { status?: unknown; text?: unknown } }).hlViewError;
      retryFrame.current(
        `board.frame-${failure}`,
        failure === 'reload'
          ? 'the board left its slot: opened again'
          : `refused ${String(said?.status ?? '')} ${String(said?.text ?? '')}`.trim(),
      );
    };
    window.addEventListener('message', onMessage);
    return () => window.removeEventListener('message', onMessage);
  }, [frameRef]);
  // Loaded, and the board never said it is there: the browser's own refusal, an error page, anything but the board.
  const silentWatch = src !== null && loadedFor === frameKey && libReadyFor !== frameKey;
  useEffect(() => {
    if (!silentWatch || refusals.current >= FRAME_SILENT_TRIES) return;
    const timer = window.setTimeout(
      () => retryFrame.current('board.frame-silent', 'the frame loaded without the board'),
      FRAME_SILENT_MS,
    );
    return () => window.clearTimeout(timer);
  }, [silentWatch, frameKey]);
};
