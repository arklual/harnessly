// One socket per machine (spec §2): hello, then channels on demand with a reference count; a snapshot and events
// with seq per channel; events at or below the snapshot's seq are dropped, a gap re-subscribes with the cursor.
// A reconnect resumes by epoch + afterSeq and afterEntryId, so the feed is never reloaded whole.
//
// Pulse: a ping every 15 s; 20 s without a frame is stale, 35 s closes the socket and opens it again. Backoff
// 3 → 6 → 12 → 20 s with ±20 % jitter, reset by welcome; online and visible reconnect at once. The connection is
// an explicit union with the moment of the next try, for the banner «Нет связи · через N с · Сейчас».

import { batch } from '@/next/core/store';
import type { Clock } from '@/next/core/clock';
import {
  parseServerFrame,
  type Channel,
  type ClientFrame,
  type Connection,
  type EntryId,
  type EvFrame,
  type FrameErrorCode,
  type MachineId,
  type ServerFrame,
  type SnapshotFrame,
} from '@/next/protocol/v2';

export const PING_MS = 15000;
export const STALE_MS = 20000;
export const DEAD_MS = 35000;
export const CONNECT_MS = 10000;
export const BACKOFF_MS = [3000, 6000, 12000, 20000] as const;
export const JITTER = 0.2;

export type Welcome = Extract<ServerFrame, { f: 'welcome' }>;
export type OfflineCause = Extract<Connection, { k: 'offline' }>['cause'];

export interface Cursor {
  epoch: string;
  seq: number;
}

/** Where the stores take the frames; the client knows nothing of what they mean. */
export interface Sink {
  welcome(machine: MachineId, frame: Welcome): void;
  snapshot(machine: MachineId, frame: SnapshotFrame): void;
  event(machine: MachineId, frame: EvFrame): void;
  channelError(machine: MachineId, ch: Channel, code: FrameErrorCode): void;
  /** Where a channel stopped before: a cached chat resumes from it. */
  resumeOf(machine: MachineId, ch: Channel): { cursor: Cursor | null; lastEntryId: EntryId | null };
  noteCursor(machine: MachineId, ch: Channel, cursor: Cursor): void;
  connection(machine: MachineId, c: Connection): void;
}

/** What the client needs of a WebSocket. */
export interface SocketLike {
  send(data: string): void;
  close(code?: number, reason?: string): void;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: unknown) => void) | null;
  onerror: ((ev: unknown) => void) | null;
}

/** The browser around the socket: whether it is online, when it comes online or the tab becomes visible. */
export interface Env {
  isOnline(): boolean;
  on(event: 'online' | 'visible', fn: () => void): () => void;
}

export const browserEnv: Env = {
  isOnline: () => (typeof navigator === 'undefined' ? true : navigator.onLine !== false),
  on(event, fn) {
    if (typeof window === 'undefined') return () => {};
    if (event === 'online') {
      window.addEventListener('online', fn);
      return () => window.removeEventListener('online', fn);
    }
    const onVis = () => {
      if (document.visibilityState === 'visible') fn();
    };
    document.addEventListener('visibilitychange', onVis);
    return () => document.removeEventListener('visibilitychange', onVis);
  },
};

export interface ClientOptions {
  machine: MachineId;
  url: string;
  device: string;
  app: string;
  sink: Sink;
  clock: Clock;
  socket: (url: string) => SocketLike;
  env?: Env;
  random?: () => number;
  /** Tells why a socket would not open: the gateway is alive but the machine is not, or the person is signed out. */
  probe?: () => Promise<OfflineCause>;
}

interface Sub {
  refs: number;
  opts?: { load?: boolean };
  /** The channel's position on this connection; null until its snapshot. */
  cursor: Cursor | null;
  /** A gap was seen and a sub with the cursor went out: drop events until the replay closes the gap. */
  resyncing: boolean;
  /** The runtime refused the channel this many times in a row; the sub goes again after a step of SUB_RETRY_MS. */
  refusals?: number;
  retry?: unknown;
}

/**
 * A held channel the runtime refused (not-found, too-many-subs) is asked for again after these steps, then
 * every 30 s while it is held: a loaded machine answers a conversation's snapshot past its budget with not-found, and a
 * channel let go for that silently stops the chat's frames — the queue over the field keeps rows the agent answered
 * long ago, the commands still work. Only a malformed channel (bad-channel) is let go for good.
 */
export const SUB_RETRY_MS = [2000, 5000, 15_000, 30_000] as const;

interface SeqWaiter {
  ch: Channel;
  seq: number;
  epoch: string | null;
  resolve: (how: 'seen' | 'gone') => void;
}

export class RealtimeClient {
  readonly machine: MachineId;
  private readonly o: ClientOptions;
  private readonly env: Env;
  private readonly random: () => number;
  private ws: SocketLike | null = null;
  private conn: Connection = { k: 'connecting', attempt: 1 };
  private epoch: string | null = null;
  private failures = 0;
  private stopped = true;
  private readonly subs = new Map<Channel, Sub>();
  private waiters: SeqWaiter[] = [];
  private timers: { ping?: unknown; stale?: unknown; dead?: unknown; retry?: unknown; connect?: unknown } =
    {};
  private unlisten: (() => void)[] = [];
  /** Set by an error frame that ends the connection: unauthorized is not retried, proto is. */
  private endCause: OfflineCause | null = null;

  constructor(o: ClientOptions) {
    this.o = o;
    this.machine = o.machine;
    this.env = o.env ?? browserEnv;
    this.random = o.random ?? Math.random;
  }

  get connection(): Connection {
    return this.conn;
  }

  get currentEpoch(): string | null {
    return this.epoch;
  }

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    this.unlisten = [
      this.env.on('online', () => {
        if (this.conn.k !== 'live') this.reconnectNow();
      }),
      this.env.on('visible', () => {
        if (this.conn.k === 'offline' || this.conn.k === 'stale') this.reconnectNow();
        else if (this.conn.k === 'live') this.ping();
      }),
    ];
    this.connect(1);
  }

  stop(): void {
    this.stopped = true;
    for (const u of this.unlisten) u();
    this.unlisten = [];
    this.clearTimers();
    this.drop();
    this.settleWaiters(() => true, 'gone');
  }

  /** «Сейчас»: a new socket at once, whatever the backoff said. */
  reconnectNow(): void {
    if (this.stopped) return;
    this.clearTimers();
    this.drop();
    this.failures = 0;
    this.connect(1);
  }

  /** Holds a channel while the returned release is not called; the first holder subscribes. */
  subscribe(ch: Channel, opts?: { load?: boolean }): () => void {
    let sub = this.subs.get(ch);
    if (!sub) {
      sub = { refs: 0, opts, cursor: this.o.sink.resumeOf(this.machine, ch).cursor, resyncing: false };
      this.subs.set(ch, sub);
    }
    sub.refs += 1;
    if (sub.refs === 1 && (this.conn.k === 'live' || this.conn.k === 'stale')) this.sendSub(ch);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      this.release(ch);
    };
  }

  /** A channel that exists from now on (conversation.create): subscribed from its first event (afterSeq 0). */
  subscribeNew(ch: Channel, epoch: string): () => void {
    if (!this.subs.has(ch)) this.subs.set(ch, { refs: 0, cursor: { epoch, seq: 0 }, resyncing: false });
    return this.subscribe(ch);
  }

  isSubscribed(ch: Channel): boolean {
    return this.subs.has(ch);
  }

  /** Resolves once the channel has shown seq (the effect of a command), or when it cannot any more. */
  waitSeq(ch: Channel, seq: number): Promise<'seen' | 'gone'> {
    const sub = this.subs.get(ch);
    if (!sub) return Promise.resolve('gone');
    if (sub.cursor && sub.cursor.epoch === this.epoch && sub.cursor.seq >= seq)
      return Promise.resolve('seen');
    return new Promise((resolve) => this.waiters.push({ ch, seq, epoch: this.epoch, resolve }));
  }

  // ---- the socket

  private connect(attempt: number): void {
    if (this.stopped) return;
    this.endCause = null;
    this.setConn({ k: 'connecting', attempt });
    let ws: SocketLike;
    try {
      ws = this.o.socket(this.o.url);
    } catch {
      this.fail();
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      if (this.ws !== ws) return;
      this.send({ f: 'hello', proto: 2, device: this.o.device, app: this.o.app });
    };
    ws.onmessage = (ev) => {
      if (this.ws !== ws) return;
      this.onData(ev.data);
    };
    ws.onclose = () => {
      if (this.ws !== ws) return;
      this.ws = null;
      this.fail();
    };
    ws.onerror = () => {};
    this.timers.connect = this.o.clock.setTimeout(() => {
      if (this.ws === ws && this.conn.k === 'connecting') {
        this.drop();
        this.fail();
      }
    }, CONNECT_MS);
  }

  private onData(data: unknown): void {
    if (typeof data !== 'string') return;
    this.heard();
    const lines = data.split('\n').filter((l) => l.trim() !== '');
    batch(() => {
      for (const line of lines) {
        let frame: ServerFrame;
        try {
          frame = parseServerFrame(JSON.parse(line));
        } catch {
          // A frame outside the protocol: this page and the runtime disagree. Start over; the next socket may fit.
          this.endCause = 'proto';
          this.drop();
          this.fail();
          return;
        }
        this.onFrame(frame);
        if (!this.ws) return;
      }
    });
  }

  private onFrame(frame: ServerFrame): void {
    switch (frame.f) {
      case 'welcome':
        this.onWelcome(frame);
        return;
      case 'snapshot':
        this.onSnapshot(frame);
        return;
      case 'ev':
        this.onEvent(frame);
        return;
      case 'pong':
        return;
      case 'error':
        if (frame.ch) {
          this.o.sink.channelError(this.machine, frame.ch, frame.code);
          if (frame.code === 'bad-channel') this.forget(frame.ch);
          else this.subAgain(frame.ch);
          this.settleWaiters((w) => w.ch === frame.ch, 'gone');
          return;
        }
        this.endCause = frame.code === 'unauthorized' ? 'unauthorized' : 'proto';
        this.drop();
        this.fail();
    }
  }

  private onWelcome(w: Welcome): void {
    this.o.clock.clearTimeout(this.timers.connect);
    const epochChanged = this.epoch !== null && this.epoch !== w.epoch;
    this.epoch = w.epoch;
    this.failures = 0;
    this.o.sink.welcome(this.machine, w);
    this.setConn({ k: 'live', since: this.o.clock.now() });
    if (epochChanged) this.settleWaiters(() => true, 'gone');
    for (const [ch, sub] of this.subs) {
      // A channel resumes from where this page stopped: the cursor it had on the last connection, or the one the
      // cache kept. The runtime decides between a replay, a snapshot after afterEntryId, and a full one; a
      // conversation's replay ends with a snapshot "after" it that adds nothing, so the page knows it caught up.
      sub.resyncing = false;
      this.o.clock.clearTimeout(sub.retry);
      sub.retry = undefined;
      if (!sub.cursor) sub.cursor = this.o.sink.resumeOf(this.machine, ch).cursor;
      this.sendSub(ch);
    }
    this.schedulePing();
  }

  private onSnapshot(s: SnapshotFrame): void {
    const sub = this.subs.get(s.ch);
    if (!sub || !this.epoch) return;
    sub.cursor = { epoch: this.epoch, seq: s.seq };
    sub.resyncing = false;
    sub.refusals = 0;
    this.o.sink.snapshot(this.machine, s);
    this.o.sink.noteCursor(this.machine, s.ch, sub.cursor);
    this.settleWaiters((w) => w.ch === s.ch && w.seq <= s.seq, 'seen');
  }

  private onEvent(ev: EvFrame): void {
    const sub = this.subs.get(ev.ch);
    // No position on this epoch yet: the channel's snapshot (or replay from afterSeq 0) has not started.
    if (!sub || !sub.cursor || sub.cursor.epoch !== this.epoch) return;
    if (ev.seq <= sub.cursor.seq) return;
    if (ev.seq > sub.cursor.seq + 1) {
      if (!sub.resyncing) {
        sub.resyncing = true;
        this.sendSub(ev.ch);
      }
      return;
    }
    sub.resyncing = false;
    sub.cursor = { epoch: sub.cursor.epoch, seq: ev.seq };
    this.o.sink.event(this.machine, ev);
    this.o.sink.noteCursor(this.machine, ev.ch, sub.cursor);
    this.settleWaiters((w) => w.ch === ev.ch && w.seq <= ev.seq, 'seen');
  }

  private sendSub(ch: Channel): void {
    const sub = this.subs.get(ch);
    if (!sub) return;
    const frame: Extract<ClientFrame, { f: 'sub' }> = { f: 'sub', ch };
    if (sub.cursor) {
      frame.epoch = sub.cursor.epoch;
      frame.afterSeq = sub.cursor.seq;
    }
    const last = this.o.sink.resumeOf(this.machine, ch).lastEntryId;
    if (last && ch.startsWith('conv:')) frame.afterEntryId = last;
    if (sub.opts) frame.opts = sub.opts;
    this.send(frame);
  }

  /** The runtime refused a held channel: it is asked for again a step later, as long as someone holds it. */
  private subAgain(ch: Channel): void {
    const sub = this.subs.get(ch);
    if (!sub) return;
    this.o.clock.clearTimeout(sub.retry);
    const n = sub.refusals ?? 0;
    sub.refusals = n + 1;
    sub.retry = this.o.clock.setTimeout(
      () => {
        sub.retry = undefined;
        if (this.subs.get(ch) !== sub || this.stopped) return;
        if (this.conn.k === 'live' || this.conn.k === 'stale') this.sendSub(ch);
      },
      SUB_RETRY_MS[Math.min(n, SUB_RETRY_MS.length - 1)],
    );
  }

  private forget(ch: Channel): void {
    const sub = this.subs.get(ch);
    if (!sub) return;
    this.o.clock.clearTimeout(sub.retry);
    this.subs.delete(ch);
  }

  private release(ch: Channel): void {
    const sub = this.subs.get(ch);
    if (!sub) return;
    sub.refs -= 1;
    if (sub.refs > 0) return;
    this.o.clock.clearTimeout(sub.retry);
    this.subs.delete(ch);
    if (this.ws && this.conn.k !== 'connecting') this.send({ f: 'unsub', ch });
    this.settleWaiters((w) => w.ch === ch, 'gone');
  }

  private send(frame: ClientFrame): void {
    try {
      this.ws?.send(JSON.stringify(frame));
    } catch {}
  }

  private ping(): void {
    this.send({ f: 'ping', t: this.o.clock.now() });
  }

  private schedulePing(): void {
    this.o.clock.clearTimeout(this.timers.ping);
    this.timers.ping = this.o.clock.setTimeout(() => {
      if (!this.ws) return;
      this.ping();
      this.schedulePing();
    }, PING_MS);
  }

  /** Any frame: the connection is alive; the silence timers start again. */
  private heard(): void {
    const { clock } = this.o;
    clock.clearTimeout(this.timers.stale);
    clock.clearTimeout(this.timers.dead);
    if (this.conn.k === 'stale') this.setConn({ k: 'live', since: clock.now() });
    this.timers.stale = clock.setTimeout(() => {
      if (this.conn.k === 'live') this.setConn({ k: 'stale', since: clock.now() });
    }, STALE_MS);
    this.timers.dead = clock.setTimeout(() => {
      // Silence past 35 s: this socket is gone even if the browser has not noticed. A new one at once.
      this.drop();
      this.connect(1);
    }, DEAD_MS);
  }

  /** Closes the socket without hearing about it. */
  private drop(): void {
    const ws = this.ws;
    this.ws = null;
    if (ws) {
      ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
      try {
        ws.close();
      } catch {}
    }
  }

  private fail(): void {
    if (this.stopped) return;
    this.clearTimers();
    const now = this.o.clock.now();
    // Without a probe every failure is the network's; the probe tells a sleeping machine from a dead link.
    const cause: OfflineCause = this.endCause ?? 'network';
    if (cause === 'unauthorized') {
      this.setConn({ k: 'offline', retryAt: Number.POSITIVE_INFINITY, cause });
      return;
    }
    const base = BACKOFF_MS[Math.min(this.failures, BACKOFF_MS.length - 1)];
    const delay = Math.round(base * (1 + (this.random() * 2 - 1) * JITTER));
    this.failures += 1;
    const attempt = this.failures + 1;
    this.setConn({ k: 'offline', retryAt: now + delay, cause });
    this.timers.retry = this.o.clock.setTimeout(() => this.connect(attempt), delay);
    if (!this.endCause && this.env.isOnline() && this.o.probe) {
      const at = this.conn;
      void this.o.probe().then((found) => {
        if (this.conn !== at || this.conn.k !== 'offline') return;
        if (found === 'unauthorized') {
          this.o.clock.clearTimeout(this.timers.retry);
          this.setConn({ k: 'offline', retryAt: Number.POSITIVE_INFINITY, cause: found });
        } else if (found !== this.conn.cause) {
          this.setConn({ ...this.conn, cause: found });
        }
      });
    }
  }

  private clearTimers(): void {
    const { clock } = this.o;
    for (const t of Object.values(this.timers)) clock.clearTimeout(t);
    this.timers = {};
  }

  private setConn(c: Connection): void {
    this.conn = c;
    this.o.sink.connection(this.machine, c);
  }

  private settleWaiters(match: (w: SeqWaiter) => boolean, how: 'seen' | 'gone'): void {
    if (this.waiters.length === 0) return;
    const keep: SeqWaiter[] = [];
    for (const w of this.waiters) {
      if (match(w)) w.resolve(how);
      else keep.push(w);
    }
    this.waiters = keep;
  }
}
