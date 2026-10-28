// What a guest looked at: POST <link>/seen {guestId, board, ms} per board on screen. The gateway keeps it only for a link
// whose owner turned «Кто и сколько смотрел» on, and takes the boards the link's index lists (the same path strings).
// The page counts the time a board was on screen while the tab was visible, and reports it when the board changes, the
// tab hides or the page goes. Pure but for the clock and the send, both given.

/** The most the gateway takes for one report. */
export const SEEN_MAX_MS = 120_000;
/** Under this a board only flashed by; nothing is reported. */
export const SEEN_MIN_MS = 1_000;

export interface ISeenReport {
  board: string;
  ms: number;
}

export interface ISeenTracker {
  /** The board now on screen (null: nothing, or the tab is hidden). The one before is reported. */
  show: (board: string | null) => void;
  /** Reports what is counted so far and goes on counting the same board. */
  flush: () => void;
}

export const createSeenTracker = (now: () => number, send: (report: ISeenReport) => void): ISeenTracker => {
  let board: string | null = null;
  let since = 0;
  const report = () => {
    if (board === null) return;
    let ms = now() - since;
    // A long stay goes in pieces the gateway takes.
    while (ms >= SEEN_MIN_MS) {
      const part = Math.min(ms, SEEN_MAX_MS);
      send({ board, ms: Math.round(part) });
      ms -= part;
    }
    // What was too short to report stays counted for the next time.
    since = now() - ms;
  };
  return {
    show: (next) => {
      if (next === board) return;
      report();
      board = next;
      since = now();
    },
    flush: report,
  };
};

/** Sends a report with the guest's id; a failed one is dropped (a count is not worth a message to the guest). */
export const sendSeen = (url: string, guestId: string, r: ISeenReport): void => {
  const body = JSON.stringify({ guestId, board: r.board, ms: r.ms });
  if (typeof navigator.sendBeacon === 'function') {
    if (navigator.sendBeacon(url, new Blob([body], { type: 'application/json' }))) return;
  }
  void fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body,
    keepalive: true,
  }).catch(() => undefined);
};
