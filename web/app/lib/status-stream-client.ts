import { homeAppPath, scopedAppPath } from '@/lib/app-path';
import { openEventStream, STREAM_CLOSED, type IEventStream } from '@/lib/event-stream';
import { watchSocket } from '@/lib/ws-heartbeat';

/**
 * Statuses (and the sync socket, `sync:` keys) read from the cluster instead of a socket into the machine: one EventSource per page for every
 * machine it shows, resumed by the browser itself from its last event after any break. A machine whose switch is off,
 * or a gateway without the stream, answers `off` / 204, and that machine keeps its status socket as before.
 */

export const DEFAULT_MACHINE_KEY = '@';
const STREAM_PATH = '/api/platform/stream';
const COMMAND_PATH = '/api/platform/stream/command';
// After a 204 a machine's next connect tries the stream again only this much later.
const RETRY_STREAM_MS = 5 * 60_000;
// A stream that brought nothing in this time is not trusted (a proxy holding the response back): the socket takes over.
const FIRST_EVENT_MS = 8_000;

interface IStreamListener {
  onLive: () => void;
  onMessage: (raw: string) => void;
  onOff: () => void;
}

type TMode = 'stream' | 'socket';

const listeners = new Map<string, Set<IStreamListener>>();
const live = new Set<string>();
// What a live machine brought since its last full status, for a listener that joins it later (the shell
// starting to draw a machine whose statuses the sidebar already reads): the stream is not reopened for a known key.
const SINCE_SYNC_MAX = 2_000;
const sinceSync = new Map<string, string[]>();
const remember = (key: string, raw: string) => {
  if (key.startsWith('sync:')) return;
  if (raw.startsWith('{"type":"status:sync"')) {
    sinceSync.set(key, [raw]);
    return;
  }
  const kept = sinceSync.get(key);
  if (!kept) return;
  if (kept.length >= SINCE_SYNC_MAX) sinceSync.delete(key);
  else kept.push(raw);
};
const modes = new Map<string, { mode: TMode; until: number }>();
let cursor = '';
let source: IEventStream | null = null;
let sourceKeys = '';
let reopenQueued = false;

const modeOf = (key: string): TMode => {
  const known = modes.get(key);
  if (!known || (known.mode === 'socket' && Date.now() > known.until)) return 'stream';
  return known.mode;
};

const goSocket = (key: string) => {
  modes.set(key, { mode: 'socket', until: Date.now() + RETRY_STREAM_MS });
  live.delete(key);
  sinceSync.delete(key);
  for (const listener of [...(listeners.get(key) ?? [])]) listener.onOff();
};

const wantedKeys = () =>
  [...listeners.keys()].filter((key) => listeners.get(key)?.size && modeOf(key) === 'stream').sort();

const reopen = () => {
  reopenQueued = false;
  const keys = wantedKeys();
  const joined = keys.join(',');
  if (joined === sourceKeys && source && source.readyState !== STREAM_CLOSED) return;
  source?.close();
  source = null;
  sourceKeys = joined;
  if (!keys.length) return;
  const params = new URLSearchParams({ machines: joined });
  if (cursor) params.set('last', cursor);
  // The page's stream, so `@` stays the page's own machine while the shell draws another one's conversation.
  const opened = openEventStream(homeAppPath(`${STREAM_PATH}?${params}`));
  source = opened;
  const goLive = (key: string) => {
    const targets = listeners.get(key);
    if (!targets || live.has(key)) return targets;
    live.add(key);
    for (const listener of [...targets]) listener.onLive();
    return targets;
  };
  const onEntry = (event: Event) => {
    const { data, lastEventId } = event as MessageEvent<string>;
    if (lastEventId) cursor = lastEventId;
    const at = data.indexOf('\n');
    if (at < 0) return;
    const targets = goLive(data.slice(0, at));
    if (!targets) return;
    const raw = data.slice(at + 1);
    remember(data.slice(0, at), raw);
    for (const listener of [...targets]) listener.onMessage(raw);
  };
  opened.addEventListener('status', onEntry);
  opened.addEventListener('sync', onEntry);
  opened.addEventListener('live', (event) => {
    const { data, lastEventId } = event as MessageEvent<string>;
    if (lastEventId) cursor = lastEventId;
    goLive(data);
  });
  opened.addEventListener('off', (event) => goSocket((event as MessageEvent<string>).data));
  opened.addEventListener('error', () => {
    if (source !== opened || opened.readyState !== STREAM_CLOSED) return;
    // Closed for good (204, an error status): every machine of this source goes back to its socket for a while.
    source = null;
    sourceKeys = '';
    for (const key of keys) goSocket(key);
  });
};

const scheduleReopen = () => {
  if (reopenQueued) return;
  reopenQueued = true;
  queueMicrotask(reopen);
};

const subscribe = (key: string, listener: IStreamListener) => {
  let set = listeners.get(key);
  if (!set) listeners.set(key, (set = new Set()));
  set.add(listener);
  if (live.has(key)) {
    const replay = [...(sinceSync.get(key) ?? [])];
    queueMicrotask(() => {
      if (!set?.has(listener)) return;
      listener.onLive();
      for (const raw of replay) listener.onMessage(raw);
    });
  }
  scheduleReopen();
  return () => {
    set?.delete(listener);
    if (!set?.size) {
      listeners.delete(key);
      live.delete(key);
      sinceSync.delete(key);
      scheduleReopen();
    }
  };
};

export interface IStatusSocket {
  readonly readyState: number;
  onopen: (() => void) | null;
  onmessage: ((event: { data: string }) => void) | null;
  onclose: (() => void) | null;
  onerror: (() => void) | null;
  send: (data: string) => void;
  close: () => void;
}

/**
 * What the status hooks hold instead of a WebSocket: the cluster stream while the machine is switched to it, its own
 * status socket otherwise. A break of the stream is not a close: the browser resumes it and nothing is redrawn.
 */
class StatusSocket implements IStatusSocket {
  readyState: number = WebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private socket: WebSocket | null = null;
  private unsubscribe: (() => void) | null = null;
  private closed = false;
  private firstEvent: ReturnType<typeof setTimeout> | null = null;

  constructor(
    private readonly socketUrl: string,
    private readonly key: string,
    private readonly machineId: string | undefined,
    private readonly watchName: string,
  ) {
    if (typeof EventSource === 'undefined' || modeOf(key) === 'socket') {
      this.openSocket();
      return;
    }
    this.firstEvent = setTimeout(() => {
      if (this.readyState === WebSocket.CONNECTING) goSocket(key);
    }, FIRST_EVENT_MS);
    this.unsubscribe = subscribe(key, {
      onLive: () => {
        if (this.firstEvent) clearTimeout(this.firstEvent);
        if (this.closed || this.readyState === WebSocket.OPEN) return;
        this.readyState = WebSocket.OPEN;
        this.onopen?.();
      },
      onMessage: (raw) => {
        if (!this.closed) this.onmessage?.({ data: raw });
      },
      onOff: () => {
        if (this.firstEvent) clearTimeout(this.firstEvent);
        this.unsubscribe?.();
        this.unsubscribe = null;
        if (!this.closed) this.openSocket();
      },
    });
  }

  private openSocket() {
    const socket = new WebSocket(this.socketUrl);
    this.socket = socket;
    watchSocket(socket, this.watchName);
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
    if (this.key.startsWith('sync:')) return;
    // The stream already brought the last sync and everything after it. Asked through the cluster, a sync would be
    // made by the runtime and sent to every page of the person, on each layout change of each of them.
    if (data.includes('"status:request-sync"')) return;
    void fetch(this.machineId ? scopedAppPath(COMMAND_PATH, this.machineId) : homeAppPath(COMMAND_PATH), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: data,
      keepalive: true,
    }).catch(() => undefined);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    if (this.firstEvent) clearTimeout(this.firstEvent);
    this.unsubscribe?.();
    this.unsubscribe = null;
    this.readyState = WebSocket.CLOSED;
    if (this.socket) this.socket.close();
    const onclose = this.onclose;
    queueMicrotask(() => onclose?.());
  }
}

/** `machineId` names another machine of the person; without it the page's own (`?machine=` or the default). */
export const openStatusSocket = (
  socketUrl: string,
  machineId: string | undefined,
  watchName: string,
): IStatusSocket => new StatusSocket(socketUrl, machineId ?? DEFAULT_MACHINE_KEY, machineId, watchName);

/** `sync-stream`: the runtime's sync socket the same way; a page never sends it anything but pings. */
export const openSyncSocket = (socketUrl: string, machineId: string | undefined): IStatusSocket =>
  new StatusSocket(socketUrl, `sync:${machineId ?? DEFAULT_MACHINE_KEY}`, machineId, 'sync');
