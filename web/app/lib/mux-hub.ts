/**
 * The page's long streams (statuses, sync, every open timeline) over one WebSocket instead of one HTTP
 * connection each: the page's origin may speak HTTP/1.1 only, where a browser keeps at most six connections to a host for all
 * its tabs, and three tabs with two streams each left every other request of every tab waiting. A WebSocket is not
 * counted against those six. The gateway serves each channel exactly as it serves the SSE request it names
 * (`/agents/api/platform/mux`), so a channel carries the same bytes, ids and ends as the stream it replaces.
 */

export const MUX_PATH = '/api/platform/mux';

export type TMuxClientFrame =
  { o: 'open'; c: number; p: string; l: string | null } | { o: 'close'; c: number };

export type TMuxChannelFrame =
  /** The status the SSE response would have had. */
  | { h: number }
  /** A piece of the response body, as written. */
  | { d: string }
  /** The response ended. */
  | { e: 1 };

export type TMuxServerFrame = TMuxChannelFrame & { c: number };

export interface IMuxSink {
  head: (status: number) => void;
  data: (chunk: string) => void;
  /** The server ended the response; an EventSource would reconnect. */
  end: () => void;
  /** The connection broke under the channel; an EventSource would reconnect. */
  drop: () => void;
  /** The mux cannot be used here: the stream has to go its own way. */
  unavailable: () => void;
}

export interface IMuxHub {
  /** `null` when the mux is known to be unavailable. */
  open: (path: string, last: string | null, sink: IMuxSink) => number | null;
  close: (channel: number) => void;
}

const OPEN = 1;
// Every stream on the gateway says something at least every 15 s; a socket quiet for longer has died on the way.
const SILENCE_MS = 45_000;
const IDLE_CLOSE_MS = 30_000;
// After a refusal the page keeps its own streams this long, then tries the mux again.
export const MUX_RETRY_MS = 5 * 60_000;

export const createMuxHub = (
  url: string,
  openSocket: (url: string) => WebSocket,
  now: () => number = Date.now,
): IMuxHub => {
  const channels = new Map<number, { path: string; last: string | null; sink: IMuxSink; sent: boolean }>();
  let socket: WebSocket | null = null;
  let opened = false;
  let unavailableUntil = 0;
  let nextId = 1;
  let lastHeard = 0;
  let watchdog: ReturnType<typeof setInterval> | null = null;
  let idle: ReturnType<typeof setTimeout> | null = null;

  const send = (frame: TMuxClientFrame) => socket?.readyState === OPEN && socket.send(JSON.stringify(frame));

  const sendOpens = () => {
    for (const [c, channel] of channels) {
      if (channel.sent) continue;
      channel.sent = true;
      send({ o: 'open', c, p: channel.path, l: channel.last });
    }
  };

  const giveUp = () => {
    unavailableUntil = now() + MUX_RETRY_MS;
    const sinks = [...channels.values()].map((channel) => channel.sink);
    channels.clear();
    socket?.close();
    for (const sink of sinks) sink.unavailable();
  };

  const stopTimers = () => {
    if (watchdog) clearInterval(watchdog);
    watchdog = null;
    if (idle) clearTimeout(idle);
    idle = null;
  };

  const connect = () => {
    if (socket) return;
    let current: WebSocket;
    try {
      current = openSocket(url);
    } catch {
      giveUp();
      return;
    }
    socket = current;
    opened = false;
    current.onopen = () => {
      if (socket !== current) return;
      opened = true;
      lastHeard = now();
      sendOpens();
    };
    current.onmessage = (event) => {
      if (socket !== current || typeof event.data !== 'string') return;
      lastHeard = now();
      let frame: TMuxServerFrame;
      try {
        frame = JSON.parse(event.data) as TMuxServerFrame;
      } catch {
        return;
      }
      const channel = channels.get(frame.c);
      if (!channel) return;
      if ('h' in frame) channel.sink.head(frame.h);
      else if ('d' in frame) channel.sink.data(frame.d);
      else if ('e' in frame) {
        channels.delete(frame.c);
        channel.sink.end();
      }
    };
    current.onerror = () => undefined;
    current.onclose = () => {
      if (socket !== current) return;
      socket = null;
      stopTimers();
      // Refused before it ever opened (an old gateway, no cluster bundle): this page uses its streams instead.
      if (!opened) return giveUp();
      const sinks = [...channels.values()].map((channel) => channel.sink);
      channels.clear();
      for (const sink of sinks) sink.drop();
    };
    watchdog = setInterval(() => {
      if (socket === current && opened && channels.size && now() - lastHeard > SILENCE_MS) current.close();
    }, SILENCE_MS / 3);
  };

  return {
    open: (path, last, sink) => {
      if (now() < unavailableUntil) return null;
      if (idle) clearTimeout(idle);
      idle = null;
      const c = nextId;
      nextId += 1;
      channels.set(c, { path, last, sink, sent: false });
      connect();
      if (now() < unavailableUntil) return null;
      if (opened) sendOpens();
      return c;
    },
    close: (c) => {
      const channel = channels.get(c);
      if (!channel) return;
      channels.delete(c);
      if (channel.sent) send({ o: 'close', c });
      if (channels.size || !socket) return;
      idle = setTimeout(() => {
        idle = null;
        if (channels.size) return;
        const idleSocket = socket;
        socket = null;
        stopTimers();
        idleSocket?.close();
      }, IDLE_CLOSE_MS);
    },
  };
};
