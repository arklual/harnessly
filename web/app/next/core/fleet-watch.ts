// How an open page learns that the person's list of machines changed (a machine added, switched on, renamed,
// registered again, or the person let into or out of a space): the hub's fleet stream, GET /api/v2/machines/events.
// The stream says only «read the list again»; the list itself is GET /api/v2/machines, so a lost event costs a
// refetch and never a wrong list.
//
// The page's sockets go to the machines, not to the hub, and a person with no machine has none: the stream is the one
// connection to the hub itself. It is held only while the tab is in view. A hidden tab lets it go, and coming back
// reads the list and opens it again, which is also the safety net for whatever the stream missed. There is no timer.

export interface FleetSource {
  /** 0 connecting, 1 open, 2 closed (EventSource). */
  readonly readyState: number;
  onopen: (() => void) | null;
  onerror: (() => void) | null;
  addEventListener(type: string, fn: () => void): void;
  close(): void;
}

interface VisibilityDoc {
  readonly visibilityState: string;
  addEventListener(type: 'visibilitychange', fn: () => void): void;
  removeEventListener(type: 'visibilitychange', fn: () => void): void;
}

interface OnlineWindow {
  addEventListener(type: 'online', fn: () => void): void;
  removeEventListener(type: 'online', fn: () => void): void;
}

export interface FleetWatchOptions {
  url: string;
  /** Reads the list again; the caller keeps one reading at a time. */
  refresh: () => void;
  /** Opens the stream; null where there is no EventSource. */
  source?: (url: string) => FleetSource | null;
  doc?: VisibilityDoc | null;
  win?: OnlineWindow | null;
}

export interface FleetWatch {
  /** The hub answers again (a machine's socket came back): the stream is opened if it is not. */
  nudge(): void;
  stop(): void;
}

const CLOSED = 2;

const browserSource = (url: string): FleetSource | null =>
  typeof EventSource === 'undefined'
    ? null
    : (new EventSource(url, { withCredentials: true }) as unknown as FleetSource);

export function watchFleet(o: FleetWatchOptions): FleetWatch {
  const doc = o.doc === undefined ? (typeof document === 'undefined' ? null : document) : o.doc;
  const win = o.win === undefined ? (typeof window === 'undefined' ? null : window) : o.win;
  const open = o.source ?? browserSource;
  let es: FleetSource | null = null;
  let stopped = false;
  /** The stream has been open on this page before: whatever happened while it was not is unknown. */
  let wasOpen = false;

  const visible = () => !doc || doc.visibilityState !== 'hidden';

  const close = () => {
    es?.close();
    es = null;
  };

  const connect = () => {
    if (stopped || es || !visible()) return;
    const mine = open(o.url);
    if (!mine) return;
    es = mine;
    mine.addEventListener('fleet', () => {
      if (es === mine) o.refresh();
    });
    mine.onopen = () => {
      if (es !== mine) return;
      // The browser's own reconnect, or ours: events may have been missed in between.
      if (wasOpen) o.refresh();
      wasOpen = true;
    };
    mine.onerror = () => {
      // A network error is retried by the browser; an answer that is not a stream (the hub restarting behind its
      // proxy, a session that ended) closes it for good, and the next way in opens another.
      if (es === mine && mine.readyState === CLOSED) close();
    };
  };

  const onVisibility = () => {
    if (!visible()) return close();
    o.refresh();
    connect();
  };
  const onOnline = () => {
    if (!visible()) return;
    o.refresh();
    connect();
  };

  doc?.addEventListener('visibilitychange', onVisibility);
  win?.addEventListener('online', onOnline);
  connect();

  return {
    nudge: connect,
    stop() {
      stopped = true;
      doc?.removeEventListener('visibilitychange', onVisibility);
      win?.removeEventListener('online', onOnline);
      close();
    },
  };
}
