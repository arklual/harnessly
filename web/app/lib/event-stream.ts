import { prefixAppPath } from '@/lib/app-path';
import { createMuxHub, MUX_PATH, MUX_RETRY_MS, type IMuxSink } from '@/lib/mux-hub';
import { createSseParser, type ISseParser } from '@/lib/sse-parser';
import type { TMuxPortReply, TMuxPortRequest } from '@/workers/mux.worker';

/**
 * What the status and timeline clients hold instead of an `EventSource`: the same events, ids, reconnects and
 * failures, carried over the mux (`mux-hub.ts`) — shared by every tab through a SharedWorker where the browser has one,
 * one socket per page where it does not — and a plain EventSource where the mux is refused or missing.
 */

export const STREAM_CONNECTING = 0;
export const STREAM_OPEN = 1;
export const STREAM_CLOSED = 2;

export interface IEventStream {
  readonly readyState: number;
  addEventListener: (type: string, listener: (event: MessageEvent<string>) => void) => void;
  close: () => void;
}

interface IChannel {
  close: () => void;
}

interface ITransport {
  /** `null` when the mux cannot carry it: the stream goes as a plain EventSource. */
  open: (path: string, last: string | null, sink: IMuxSink) => IChannel | null;
}

const OFF_KEY = 'hl-mux-off-until';
const DEFAULT_RETRY_MS = 3_000;
const BEAT_MS = 20_000;

/** How the page's last stream went: the speed telemetry tells mux opens from SSE ones by it. */
let lastVia: 'mux' | 'sse' | null = null;
export const streamVia = (): 'mux' | 'sse' | null => lastVia;

const muxOff = (): boolean => {
  try {
    return Number(localStorage.getItem(OFF_KEY) ?? 0) > Date.now();
  } catch {
    return false;
  }
};

const markMuxOff = () => {
  try {
    localStorage.setItem(OFF_KEY, String(Date.now() + MUX_RETRY_MS));
  } catch {
    // Without storage the next page tries the mux again; it is refused as quickly.
  }
};

const muxUrl = () => {
  const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  // The mux is the gateway's, never a machine's: no `?machine=` on it, each channel names its own.
  return `${protocol}//${location.host}${prefixAppPath(MUX_PATH)}`;
};

const workerTransport = (): ITransport | null => {
  let port: MessagePort;
  try {
    const worker = new SharedWorker(new URL('../workers/mux.worker.ts', import.meta.url), {
      type: 'module',
      name: 'hl-mux',
    });
    port = worker.port;
  } catch {
    return null;
  }
  const sinks = new Map<number, IMuxSink>();
  let nextId = 1;
  const post = (message: TMuxPortRequest) => port.postMessage(message);
  port.onmessage = ({ data }: MessageEvent<TMuxPortReply>) => {
    const sink = sinks.get(data.id);
    if (!sink) return;
    if (data.k === 'head') sink.head(data.v);
    else if (data.k === 'data') sink.data(data.v);
    else {
      sinks.delete(data.id);
      sink[data.k]();
    }
  };
  port.start();
  setInterval(() => post({ t: 'beat' }), BEAT_MS);
  window.addEventListener('pagehide', (event) => {
    if (!(event as PageTransitionEvent).persisted) post({ t: 'bye' });
  });
  const url = muxUrl();
  return {
    open: (path, last, sink) => {
      const id = nextId;
      nextId += 1;
      sinks.set(id, sink);
      post({ t: 'open', id, url, path, last });
      return {
        close: () => {
          if (sinks.delete(id)) post({ t: 'close', id });
        },
      };
    },
  };
};

const pageTransport = (): ITransport => {
  const hub = createMuxHub(muxUrl(), (address) => new WebSocket(address));
  return {
    open: (path, last, sink) => {
      const channel = hub.open(path, last, sink);
      return channel === null ? null : { close: () => hub.close(channel) };
    },
  };
};

let transport: ITransport | null | undefined;
const transportOf = (): ITransport | null => {
  if (transport !== undefined) return transport;
  if (typeof document === 'undefined' || typeof WebSocket === 'undefined') return (transport = null);
  transport = (typeof SharedWorker !== 'undefined' ? workerTransport() : null) ?? pageTransport();
  return transport;
};

const withLast = (url: string, last: string) => {
  if (!last) return url;
  const parsed = new URL(url, location.href);
  parsed.searchParams.set('last', last);
  return `${parsed.pathname}${parsed.search}`;
};

class MuxEventStream implements IEventStream {
  private state = STREAM_CONNECTING;
  private readonly listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>();
  private lastEventId = '';
  private retryMs = DEFAULT_RETRY_MS;
  private channel: IChannel | null = null;
  private parser: ISseParser | null = null;
  private native: EventSource | null = null;
  private readonly forwarded = new Set<string>();
  private retry: ReturnType<typeof setTimeout> | null = null;
  private closed = false;

  constructor(
    private readonly url: string,
    private readonly via: ITransport,
  ) {
    this.connect();
  }

  get readyState() {
    return this.native ? this.native.readyState : this.state;
  }

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    let set = this.listeners.get(type);
    if (!set) this.listeners.set(type, (set = new Set()));
    set.add(listener);
    if (this.native) this.forward(type);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    this.state = STREAM_CLOSED;
    if (this.retry) clearTimeout(this.retry);
    this.channel?.close();
    this.channel = null;
    this.native?.close();
  }

  private dispatch(type: string, data = '', lastEventId = '') {
    if (this.closed) return;
    const event = new MessageEvent<string>(type, { data, lastEventId });
    for (const listener of [...(this.listeners.get(type) ?? [])]) listener(event);
  }

  private connect() {
    if (this.closed || this.native) return;
    this.state = STREAM_CONNECTING;
    const parser = createSseParser(
      (event) => {
        if (this.parser !== parser) return;
        this.lastEventId = event.lastEventId;
        this.dispatch(event.type, event.data, event.lastEventId);
      },
      (ms) => {
        this.retryMs = ms;
      },
      this.lastEventId,
    );
    this.parser = parser;
    const live = () => this.parser === parser && !this.closed;
    const channel = this.via.open(this.url, this.lastEventId || null, {
      head: (status) => {
        if (!live()) return;
        if (status === 200) {
          this.state = STREAM_OPEN;
          this.dispatch('open');
          return;
        }
        // Like an EventSource answered 204 or an error status: closed for good, said once.
        this.parser = null;
        this.channel = null;
        this.state = STREAM_CLOSED;
        this.dispatch('error');
      },
      data: (chunk) => {
        if (!live()) return;
        parser.push(chunk);
        this.lastEventId = parser.lastEventId;
      },
      end: () => live() && this.reconnect(),
      drop: () => live() && this.reconnect(),
      unavailable: () => {
        if (!live()) return;
        markMuxOff();
        this.toNative();
      },
    });
    if (!this.closed && this.parser === parser) {
      if (channel) this.channel = channel;
      else this.toNative();
    }
  }

  private reconnect() {
    this.parser = null;
    this.channel = null;
    this.state = STREAM_CONNECTING;
    this.dispatch('error');
    if (this.closed) return;
    this.retry = setTimeout(() => {
      this.retry = null;
      this.connect();
    }, this.retryMs);
  }

  private toNative() {
    if (this.native || this.closed) return;
    lastVia = 'sse';
    this.parser = null;
    this.channel = null;
    this.native = new EventSource(withLast(this.url, this.lastEventId));
    for (const type of this.listeners.keys()) this.forward(type);
  }

  private forward(type: string) {
    if (!this.native || this.forwarded.has(type)) return;
    this.forwarded.add(type);
    this.native.addEventListener(type, (event) => {
      if (this.closed) return;
      for (const listener of [...(this.listeners.get(type) ?? [])]) listener(event as MessageEvent<string>);
    });
  }
}

/** An EventSource of `url` (an app path, `/agents/api/platform/…`), over the mux while it is there. */
export const openEventStream = (url: string): IEventStream => {
  const via = muxOff() ? null : transportOf();
  lastVia = via ? 'mux' : 'sse';
  return via ? new MuxEventStream(url, via) : (new EventSource(url) as unknown as IEventStream);
};
