import { describe, expect, it } from 'vitest';
import { watchFleet, type FleetSource } from '@/next/core/fleet-watch';

class FakeSource implements FleetSource {
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;
  private listeners = new Map<string, (() => void)[]>();
  addEventListener(type: string, fn: () => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }
  close(): void {
    this.closed = true;
    this.readyState = 2;
  }
  opened(): void {
    this.readyState = 1;
    this.onopen?.();
  }
  emit(type: string): void {
    for (const fn of this.listeners.get(type) ?? []) fn();
  }
  /** The server answered with something that is not a stream: the browser gives up. */
  refused(): void {
    this.readyState = 2;
    this.onerror?.();
  }
  /** The connection dropped: the browser retries by itself. */
  dropped(): void {
    this.readyState = 0;
    this.onerror?.();
  }
}

function setup(visibility: 'visible' | 'hidden' = 'visible') {
  const sources: FakeSource[] = [];
  const handlers: Record<string, () => void> = {};
  const doc = {
    visibilityState: visibility as string,
    addEventListener: (type: string, fn: () => void) => void (handlers[type] = fn),
    removeEventListener: (type: string) => void delete handlers[type],
  };
  const win = {
    addEventListener: (type: string, fn: () => void) => void (handlers[type] = fn),
    removeEventListener: (type: string) => void delete handlers[type],
  };
  let refreshed = 0;
  const watch = watchFleet({
    url: '/api/v2/machines/events',
    refresh: () => void (refreshed += 1),
    source: () => {
      const s = new FakeSource();
      sources.push(s);
      return s;
    },
    doc,
    win,
  });
  const show = (state: 'visible' | 'hidden') => {
    doc.visibilityState = state;
    handlers.visibilitychange?.();
  };
  return { watch, sources, handlers, show, refreshed: () => refreshed };
}

describe('watchFleet', () => {
  it('reads the list again on every fleet event, and not on the first open', () => {
    const t = setup();
    expect(t.sources).toHaveLength(1);
    t.sources[0]!.opened();
    expect(t.refreshed()).toBe(0);
    t.sources[0]!.emit('fleet');
    t.sources[0]!.emit('fleet');
    expect(t.refreshed()).toBe(2);
  });

  it('a reconnect of the stream reads the list: events may have been missed', () => {
    const t = setup();
    t.sources[0]!.opened();
    t.sources[0]!.dropped();
    expect(t.sources).toHaveLength(1);
    t.sources[0]!.opened();
    expect(t.refreshed()).toBe(1);
  });

  it('a hidden tab lets the stream go; coming back reads the list and opens it again, with no timer', () => {
    const t = setup();
    t.sources[0]!.opened();
    t.show('hidden');
    expect(t.sources[0]!.closed).toBe(true);
    expect(t.refreshed()).toBe(0);
    t.show('visible');
    expect(t.refreshed()).toBe(1);
    expect(t.sources).toHaveLength(2);
    // An event of the stream that was let go changes nothing.
    t.sources[0]!.emit('fleet');
    expect(t.refreshed()).toBe(1);
  });

  it('a tab that starts hidden opens no stream until it is seen', () => {
    const t = setup('hidden');
    expect(t.sources).toHaveLength(0);
    t.show('visible');
    expect(t.sources).toHaveLength(1);
  });

  it('a refused stream is not retried by itself; a nudge, the network or the tab coming back opens another', () => {
    const t = setup();
    t.sources[0]!.refused();
    expect(t.sources).toHaveLength(1);
    t.watch.nudge();
    expect(t.sources).toHaveLength(2);
    // An open stream is left alone.
    t.watch.nudge();
    expect(t.sources).toHaveLength(2);
    t.sources[1]!.refused();
    t.handlers.online?.();
    expect(t.sources).toHaveLength(3);
    expect(t.refreshed()).toBe(1);
  });

  it('stop closes the stream and lets go of the page', () => {
    const t = setup();
    t.watch.stop();
    expect(t.sources[0]!.closed).toBe(true);
    expect(t.handlers.visibilitychange).toBeUndefined();
    t.watch.nudge();
    expect(t.sources).toHaveLength(1);
  });
});
