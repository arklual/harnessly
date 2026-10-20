import { scopedAppPath } from '@/lib/app-path';
import { openEventStream, STREAM_CLOSED, type IEventStream } from '@/lib/event-stream';
import { watchSocket } from '@/lib/ws-heartbeat';

/**
 * A conversation's timeline from the cluster as an SSE stream: after a break the browser resumes it
 * itself from its last event and gets only what it missed. A machine whose `timeline-stream` switch is off (or a
 * gateway without the stream) answers 204, and the page opens its timeline socket as before.
 */

const STREAM_PATH = '/api/platform/timeline-stream';
const COMMAND_PATH = '/api/platform/timeline-stream/command';
// After the stream was refused, the next connects of this page use the socket for this long.
const SOCKET_FOR_MS = 5 * 60_000;
// A stream that said nothing in this time is not trusted (a proxy holding it back): the socket takes over.
const FIRST_EVENT_MS = 8_000;

// By machine: a machine off the stream must not send another machine's conversations to their sockets.
const socketUntil = new Map<string, number>();
const machineOf = (socketUrl: string) => {
  try {
    return new URL(socketUrl, 'http://localhost').searchParams.get('machine') ?? '@';
  } catch {
    return '@';
  }
};

export interface ITimelineSocket {
  readonly readyState: number;
  /** The stream's position, to resume a later connection from it; `null` on a socket. */
  readonly lastEventId: string | null;
  onopen: (() => void) | null;
  onmessage: ((event: { data: string }) => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  /** The stream broke and the browser is bringing it back; the next `onopen` says it is back. */
  onreconnecting: (() => void) | null;
  send: (data: string) => void;
  close: (code?: number) => void;
}

const PING_MESSAGE = '{"type":"timeline:ping"}';

class TimelineSocket implements ITimelineSocket {
  readyState: number = WebSocket.CONNECTING;
  lastEventId: string | null = null;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onreconnecting: (() => void) | null = null;
  private source: IEventStream | null = null;
  private socket: WebSocket | null = null;
  private streamId: string | null = null;
  private closed = false;
  private heard = false;
  private firstEvent: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private readonly socketUrl: string,
    query: URLSearchParams,
    resumeFrom: string | null,
  ) {
    if (typeof EventSource === 'undefined' || Date.now() < (socketUntil.get(machineOf(socketUrl)) ?? 0)) {
      this.openSocket();
      return;
    }
    const params = new URLSearchParams(query);
    if (resumeFrom) params.set('last', resumeFrom);
    this.lastEventId = resumeFrom;
    const source = openEventStream(scopedAppPath(`${STREAM_PATH}?${params}`));
    this.source = source;
    this.firstEvent = setTimeout(() => {
      if (!this.heard) this.toSocket();
    }, FIRST_EVENT_MS);
    source.addEventListener('hello', (event) => {
      this.heard = true;
      if (this.firstEvent) clearTimeout(this.firstEvent);
      try {
        this.streamId = (JSON.parse((event as MessageEvent<string>).data) as { id?: string }).id ?? null;
      } catch {
        this.streamId = null;
      }
      if (this.closed) return;
      this.readyState = WebSocket.OPEN;
      this.onopen?.();
    });
    source.addEventListener('timeline', (event) => {
      const { data, lastEventId } = event as MessageEvent<string>;
      if (lastEventId) this.lastEventId = lastEventId;
      if (!this.closed) this.onmessage?.({ data });
    });
    source.addEventListener('ping', () => {
      if (!this.closed) this.onmessage?.({ data: PING_MESSAGE });
    });
    source.addEventListener('error', () => {
      if (this.closed || this.source !== source) return;
      if (source.readyState === STREAM_CLOSED) {
        // Refused for good (204, an error status) before it ever spoke: the socket, at once and for a while.
        if (!this.heard) return this.toSocket();
        socketUntil.set(machineOf(this.socketUrl), Date.now() + SOCKET_FOR_MS);
        this.readyState = WebSocket.CLOSED;
        this.onclose?.();
        return;
      }
      this.onreconnecting?.();
    });
  }

  private toSocket() {
    if (this.closed) return;
    socketUntil.set(machineOf(this.socketUrl), Date.now() + SOCKET_FOR_MS);
    if (this.firstEvent) clearTimeout(this.firstEvent);
    this.source?.close();
    this.source = null;
    this.lastEventId = null;
    this.openSocket();
  }

  private openSocket() {
    const socket = new WebSocket(this.socketUrl);
    this.socket = socket;
    watchSocket(socket, 'timeline');
    socket.onopen = () => {
      this.readyState = WebSocket.OPEN;
      this.onopen?.();
    };
    socket.onmessage = (event) => this.onmessage?.({ data: event.data as string });
    socket.onerror = () => this.onerror?.();
    socket.onclose = () => {
      this.readyState = WebSocket.CLOSED;
      if (!this.closed) this.onclose?.();
    };
  }

  send(data: string) {
    if (this.socket) {
      if (this.socket.readyState === WebSocket.OPEN) this.socket.send(data);
      return;
    }
    if (!this.streamId) return;
    void fetch(scopedAppPath(`${COMMAND_PATH}?id=${encodeURIComponent(this.streamId)}`), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: data,
      keepalive: true,
    }).catch(() => undefined);
  }

  close(code?: number) {
    if (this.closed) return;
    this.closed = true;
    if (this.firstEvent) clearTimeout(this.firstEvent);
    this.readyState = WebSocket.CLOSED;
    this.source?.close();
    this.source = null;
    this.socket?.close(code);
  }
}

/**
 * `socketUrl` is the timeline socket the page would open; `query` its parameters for the stream. `resumeFrom` is the
 * `lastEventId` of an earlier stream of the same conversation in the same view, so a reconnect brings no first screen.
 */
export const openTimelineSocket = (
  socketUrl: string,
  query: URLSearchParams,
  resumeFrom: string | null,
): ITimelineSocket => new TimelineSocket(socketUrl, query, resumeFrom);
