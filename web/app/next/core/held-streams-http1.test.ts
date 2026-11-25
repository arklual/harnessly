// @vitest-environment jsdom
// over HTTP/1.1 (a deployment behind Teleport) the browser keeps six connections to the site for all its
// tabs. The page's background streams read again now and then instead of holding a connection each.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FLEET_POLL_MS, watchFleet } from '@/next/core/fleet-watch';
import { SHORT_POLL_MS } from '@/next/core/net/http';
import { openSlotStream, setSlotStreamFactory } from '@/next/features/smart-notice/slot-stream';
import type { MachineId } from '@/next/protocol/v2';
import {
  dismissAllNotices,
  dismissNotice,
  noticesSnapshot,
  setNoticesPerson,
  type INotice,
} from '@/next/stores/notices';
import { startNoticesSync } from '@/next/stores/notices-sync';

const protocol = (p: string) =>
  vi
    .spyOn(performance, 'getEntriesByType')
    .mockReturnValue([{ nextHopProtocol: p } as PerformanceNavigationTiming]);

let visibility = 'visible';

beforeEach(() => {
  vi.useFakeTimers();
  visibility = 'visible';
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(
    () => visibility as DocumentVisibilityState,
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe('fleet over HTTP/1.1', () => {
  it('reads the list on a timer while in view and opens no stream', () => {
    protocol('http/1.1');
    const source = vi.fn(() => null);
    let refreshed = 0;
    const watch = watchFleet({
      url: '/e',
      refresh: () => void (refreshed += 1),
      source,
      doc: document,
      win: null,
    });
    expect(source).not.toHaveBeenCalled();
    vi.advanceTimersByTime(FLEET_POLL_MS * 2);
    expect(refreshed).toBe(2);
    visibility = 'hidden';
    document.dispatchEvent(new Event('visibilitychange'));
    vi.advanceTimersByTime(FLEET_POLL_MS * 3);
    expect(refreshed).toBe(2);
    visibility = 'visible';
    document.dispatchEvent(new Event('visibilitychange'));
    expect(refreshed).toBe(3);
    vi.advanceTimersByTime(FLEET_POLL_MS);
    expect(refreshed).toBe(4);
    watch.stop();
    vi.advanceTimersByTime(FLEET_POLL_MS * 3);
    expect(refreshed).toBe(4);
    expect(source).not.toHaveBeenCalled();
  });

  it('holds the stream over HTTP/2', () => {
    protocol('h2');
    const source = vi.fn(() => null);
    const watch = watchFleet({ url: '/e', refresh: () => undefined, source, doc: document, win: null });
    expect(source).toHaveBeenCalledTimes(1);
    watch.stop();
  });
});

describe('the slot over HTTP/1.1', () => {
  it('opens no stream: the calm poll stays', () => {
    protocol('http/1.1');
    const factory = vi.fn();
    const restore = setSlotStreamFactory(factory);
    const live = vi.fn();
    openSlotStream('m1' as MachineId, () => undefined, live)();
    expect(factory).not.toHaveBeenCalled();
    expect(live).not.toHaveBeenCalledWith(true);
    setSlotStreamFactory(restore);
  });
});

describe('notices over HTTP/1.1', () => {
  const notice = (key: string): INotice => ({
    key,
    chatId: 'c1',
    kind: 'done',
    icon: 'x',
    title: 't',
    text: 'x',
    at: 1,
  });

  beforeEach(() => {
    localStorage.clear();
    setNoticesPerson('p');
    dismissAllNotices();
  });

  it('reads the list again instead of the stream, and a read sent from here does not come back', async () => {
    protocol('http/1.1');
    let items = [{ key: 'k1', chatId: 'c1', at: 1, doc: notice('k1') }];
    let reads: { key: string }[] = [];
    let gets = 0;
    const posts: string[] = [];
    const fetchImpl = (async (url: string, init?: RequestInit) => {
      if (init?.method === 'POST') posts.push(new URL(url, 'http://x').pathname);
      else gets += 1;
      return new Response(JSON.stringify({ items, reads, settings: { rev: 0 } }), { status: 200 });
    }) as unknown as typeof fetch;
    const stream = vi.fn();
    const stop = startNoticesSync({ base: '/n', person: 'p', fetchImpl, stream, device: 'A' });
    await vi.advanceTimersByTimeAsync(0);
    expect(stream).not.toHaveBeenCalled();
    expect(noticesSnapshot().map((n) => n.key)).toEqual(['k1']);

    // Another device adds one: the next read brings it.
    items = [...items, { key: 'k2', chatId: 'c2', at: 2, doc: { ...notice('k2'), chatId: 'c2' } }];
    await vi.advanceTimersByTimeAsync(SHORT_POLL_MS);
    expect(
      noticesSnapshot()
        .map((n) => n.key)
        .sort(),
    ).toEqual(['k1', 'k2']);

    // Read here: the database has not taken the read yet, and the list still shows it unread.
    dismissNotice('k1');
    expect(posts).toContain('/n/read');
    await vi.advanceTimersByTimeAsync(SHORT_POLL_MS);
    expect(noticesSnapshot().map((n) => n.key)).toEqual(['k2']);

    // Read on another device: the next read drops it.
    reads = [{ key: 'k1' }, { key: 'k2' }];
    await vi.advanceTimersByTimeAsync(SHORT_POLL_MS);
    expect(noticesSnapshot()).toEqual([]);

    // A hidden page does not ask.
    visibility = 'hidden';
    const before = gets;
    await vi.advanceTimersByTimeAsync(SHORT_POLL_MS * 3);
    expect(gets).toBe(before);
    stop();
  });
});
