import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { startCentralBeat, type CentralBeat } from '@/next/bindings/central-beat';
import { setPageClock, systemClock } from '@/next/core/clock';

describe('the beat of the cluster list', () => {
  let visible = true;
  let asked: boolean[] = [];
  let beat: CentralBeat;

  beforeEach(() => {
    vi.useFakeTimers();
    setPageClock(systemClock);
    visible = true;
    asked = [];
    beat = startCentralBeat({
      refresh: (remerge) => asked.push(remerge),
      everyMs: 30_000,
      minGapMs: 10_000,
      visible: () => visible,
    });
  });
  afterEach(() => {
    beat.stop();
    vi.useRealTimers();
  });

  it('asks on mount and on every beat', () => {
    expect(asked).toEqual([false]);
    vi.advanceTimersByTime(60_000);
    expect(asked).toEqual([false, false, false]);
  });

  it('makes one request of the links that change together, no sooner than the gap', () => {
    vi.advanceTimersByTime(1_000);
    for (let i = 0; i < 5; i++) {
      beat.linksChanged();
      vi.advanceTimersByTime(500);
    }
    expect(asked).toEqual([false]);
    vi.advanceTimersByTime(6_500);
    // 10 s after the mount's request: one request for the five changes, merged anew.
    expect(asked).toEqual([false, true]);
    // The beat counts from that request.
    vi.advanceTimersByTime(29_999);
    expect(asked).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(asked).toEqual([false, true, false]);
  });

  it('asks at once for a link that changes after the gap', () => {
    vi.advanceTimersByTime(12_000);
    beat.linksChanged();
    expect(asked).toEqual([false, true]);
    beat.linksChanged();
    expect(asked).toHaveLength(2);
  });

  it('asks nothing from a hidden tab and catches up when it is shown', () => {
    visible = false;
    vi.advanceTimersByTime(12_000);
    beat.linksChanged();
    vi.advanceTimersByTime(90_000);
    expect(asked).toEqual([false]);
    visible = true;
    beat.visibilityChanged();
    expect(asked).toEqual([false, true]);
    // Shown again with nothing owed: no request.
    beat.visibilityChanged();
    expect(asked).toHaveLength(2);
  });

  it('keeps the gap for a link that changed while the tab was hidden for a moment', () => {
    visible = false;
    vi.advanceTimersByTime(2_000);
    beat.linksChanged();
    visible = true;
    beat.visibilityChanged();
    expect(asked).toEqual([false]);
    vi.advanceTimersByTime(8_000);
    expect(asked).toEqual([false, true]);
  });

  it('stops', () => {
    beat.stop();
    vi.advanceTimersByTime(120_000);
    expect(asked).toEqual([false]);
  });
});
