export type SocketState = 'connecting' | 'open' | 'closed';

export interface SocketLike {
  binaryType: BinaryType;
  readyState: number;
  onopen: ((ev: Event) => void) | null;
  onclose: ((ev: CloseEvent) => void) | null;
  onerror: ((ev: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  send(data: string | ArrayBufferLike | ArrayBufferView | Blob): void;
  close(code?: number, reason?: string): void;
}

export type SocketFactory = (url: string) => SocketLike;

export interface ReconnectingOptions {
  url: () => string;
  onMessage: (data: string | ArrayBuffer) => void;
  onOpen?: (send: (data: string | ArrayBufferLike | ArrayBufferView) => void) => void;
  onState?: (state: SocketState) => void;
  /** Return false to stop reconnecting after this close (e.g. the server said the session is gone). */
  shouldRetry?: (ev: CloseEvent) => boolean;
  binary?: boolean;
  factory?: SocketFactory;
  /** First retry delay; it doubles up to maxDelay. */
  minDelay?: number;
  maxDelay?: number;
}

const OPEN = 1;

/** A WebSocket that reopens with backoff until it is closed on purpose. */
export class ReconnectingSocket {
  private ws: SocketLike | null = null;
  private delay: number;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stopped = false;
  state: SocketState = 'connecting';

  constructor(private readonly opts: ReconnectingOptions) {
    this.delay = opts.minDelay ?? 500;
    this.connect();
  }

  private setState(s: SocketState) {
    this.state = s;
    this.opts.onState?.(s);
  }

  private connect() {
    if (this.stopped) return;
    this.setState('connecting');
    const make = this.opts.factory ?? ((u: string) => new WebSocket(u) as unknown as SocketLike);
    const ws = make(this.opts.url());
    ws.binaryType = 'arraybuffer';
    this.ws = ws;
    ws.onopen = () => {
      this.delay = this.opts.minDelay ?? 500;
      this.setState('open');
      this.opts.onOpen?.((d) => ws.send(d));
    };
    ws.onmessage = (ev) => this.opts.onMessage(ev.data as string | ArrayBuffer);
    ws.onclose = (ev) => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.setState('closed');
      if (this.stopped || (this.opts.shouldRetry && !this.opts.shouldRetry(ev))) return;
      this.timer = setTimeout(() => this.connect(), this.delay);
      this.delay = Math.min(this.delay * 2, this.opts.maxDelay ?? 10_000);
    };
    ws.onerror = () => {
      // a close follows
    };
  }

  /** Sends when open; returns false when the message was dropped. */
  send(data: string | ArrayBufferLike | ArrayBufferView): boolean {
    if (this.ws && this.ws.readyState === OPEN) {
      this.ws.send(data);
      return true;
    }
    return false;
  }

  /** Reopens now (e.g. after the page came back online). */
  reconnect() {
    clearTimeout(this.timer);
    const ws = this.ws;
    this.ws = null;
    ws?.close();
    this.connect();
  }

  close() {
    this.stopped = true;
    clearTimeout(this.timer);
    const ws = this.ws;
    this.ws = null;
    ws?.close(1000);
    this.setState('closed');
  }
}
