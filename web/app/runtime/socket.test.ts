import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ReconnectingSocket, type SocketLike } from '@/runtime/socket';

class FakeSocket implements SocketLike {
  static made: FakeSocket[] = [];
  binaryType: BinaryType = 'blob';
  readyState = 0;
  sent: unknown[] = [];
  onopen: ((ev: Event) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  constructor(readonly url: string) {
    FakeSocket.made.push(this);
  }
  send(data: unknown) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.({} as Event);
  }
  drop(code = 1006, reason = '') {
    this.readyState = 3;
    this.onclose?.({ code, reason } as CloseEvent);
  }
}

describe('ReconnectingSocket', () => {
  beforeEach(() => {
    FakeSocket.made = [];
    vi.useFakeTimers();
  });
  afterEach(() => vi.useRealTimers());

  it('reopens with a growing delay and resets it once open', () => {
    const states: string[] = [];
    let n = 0;
    const s = new ReconnectingSocket({
      url: () => `ws://x/${n++}`,
      onMessage: () => undefined,
      onState: (st) => states.push(st),
      factory: (u) => new FakeSocket(u),
      minDelay: 100,
      maxDelay: 400,
    });
    expect(FakeSocket.made.map((f) => f.url)).toEqual(['ws://x/0']);
    FakeSocket.made[0]!.drop();
    vi.advanceTimersByTime(99);
    expect(FakeSocket.made).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(FakeSocket.made).toHaveLength(2);
    FakeSocket.made[1]!.drop();
    vi.advanceTimersByTime(199);
    expect(FakeSocket.made).toHaveLength(2);
    vi.advanceTimersByTime(1);
    FakeSocket.made[2]!.open();
    expect(s.state).toBe('open');
    FakeSocket.made[2]!.drop();
    vi.advanceTimersByTime(100);
    expect(FakeSocket.made).toHaveLength(4);
    expect(states).toContain('closed');
    s.close();
  });

  it('sends only while open and greets on every open', () => {
    const greeted: number[] = [];
    const s = new ReconnectingSocket({
      url: () => 'ws://x',
      onMessage: () => undefined,
      onOpen: (send) => {
        greeted.push(1);
        send('hello');
      },
      factory: (u) => new FakeSocket(u),
    });
    expect(s.send('early')).toBe(false);
    FakeSocket.made[0]!.open();
    expect(s.send('late')).toBe(true);
    expect(FakeSocket.made[0]!.sent).toEqual(['hello', 'late']);
    expect(greeted).toHaveLength(1);
    s.close();
  });

  it('stops when told the close is final, and after close()', () => {
    const s = new ReconnectingSocket({
      url: () => 'ws://x',
      onMessage: () => undefined,
      shouldRetry: (ev) => ev.code !== 1008,
      factory: (u) => new FakeSocket(u),
    });
    FakeSocket.made[0]!.drop(1008, 'Unknown panel type');
    vi.advanceTimersByTime(60_000);
    expect(FakeSocket.made).toHaveLength(1);

    const t = new ReconnectingSocket({
      url: () => 'ws://y',
      onMessage: () => undefined,
      factory: (u) => new FakeSocket(u),
    });
    t.close();
    vi.advanceTimersByTime(60_000);
    expect(FakeSocket.made).toHaveLength(2);
    s.close();
  });

  it('passes messages through', () => {
    const got: unknown[] = [];
    const s = new ReconnectingSocket({
      url: () => 'ws://x',
      onMessage: (d) => got.push(d),
      factory: (u) => new FakeSocket(u),
    });
    FakeSocket.made[0]!.onmessage?.({ data: '{"type":"pong"}' } as MessageEvent);
    expect(got).toEqual(['{"type":"pong"}']);
    s.close();
  });
});
