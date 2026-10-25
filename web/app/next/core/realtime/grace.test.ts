import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FIRST_LINK_GRACE_MS, resetLinkGrace, shownConnection } from '@/next/core/realtime/grace';
import type { Connection, MachineId } from '@/next/protocol/v2';

const M = 'm-grace' as MachineId;
const offline = (cause: 'network' | 'machine-unreachable' | 'unauthorized' = 'network'): Connection => ({
  k: 'offline',
  retryAt: 1,
  cause,
});

let shown: Connection[] = [];
const feed = (c: Connection) => shownConnection(M, c, (_m, s) => shown.push(s));

beforeEach(() => {
  vi.useFakeTimers();
  shown = [];
});
afterEach(() => {
  resetLinkGrace();
  vi.useRealTimers();
});

describe('the first connection after a load', () => {
  it('does not claim a lost machine in its first seconds, and says so when the grace is over', () => {
    feed({ k: 'connecting', attempt: 1 });
    feed(offline('machine-unreachable'));
    expect(shown.at(-1)?.k).toBe('connecting');
    vi.advanceTimersByTime(FIRST_LINK_GRACE_MS + 10);
    expect(shown.at(-1)).toEqual(offline('machine-unreachable'));
  });

  it('shows live at once and then reads a real loss as it is', () => {
    feed({ k: 'connecting', attempt: 1 });
    feed(offline());
    feed({ k: 'live', since: 5 });
    vi.advanceTimersByTime(FIRST_LINK_GRACE_MS + 10);
    expect(shown.at(-1)?.k).toBe('live');
    feed({ k: 'stale', since: 6 });
    feed(offline());
    expect(shown.at(-1)).toEqual(offline());
  });

  it('does not hide a refusal of access', () => {
    feed({ k: 'connecting', attempt: 1 });
    feed(offline('unauthorized'));
    expect(shown.at(-1)).toEqual(offline('unauthorized'));
  });
});
