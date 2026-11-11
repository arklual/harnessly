import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createVisibilityReporter,
  trackVisibility,
  VISIBILITY_REPORT,
  type IVisibilityHost,
} from '@/lib/visibility-report';

/** The server's lifetime of a "looking" mark (VisibilityTTL in internal/push). */
const SERVER_TTL_MS = 60_000;

const host = (visible = true, focused = true) => {
  const state = { visible, focused };
  const win = new EventTarget();
  const doc = new EventTarget() as unknown as EventTarget & IVisibilityHost['document'];
  Object.defineProperty(doc, 'visibilityState', { get: () => (state.visible ? 'visible' : 'hidden') });
  Object.defineProperty(doc, 'hasFocus', { value: () => state.focused });
  return { state, window: win, document: doc };
};

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('reporting whether the person looks at the app', () => {
  it('says so once, then only confirms before the server forgets it', () => {
    const sent: boolean[] = [];
    const h = host();
    const stop = trackVisibility(
      createVisibilityReporter((v) => sent.push(v)),
      h,
    );
    expect(sent).toEqual([true]);
    vi.advanceTimersByTime(10 * 60_000);
    // Ten minutes in focus: a confirmation per pingMs, nothing else.
    expect(sent).toHaveLength(1 + Math.floor((10 * 60_000) / VISIBILITY_REPORT.pingMs));
    expect(sent.every(Boolean)).toBe(true);
    stop();
    vi.advanceTimersByTime(5 * 60_000);
    expect(sent).toHaveLength(1 + Math.floor((10 * 60_000) / VISIBILITY_REPORT.pingMs));
  });

  it('confirms within the lifetime of the mark on the server', () => {
    expect(VISIBILITY_REPORT.pingMs).toBeLessThan(SERVER_TTL_MS);
    expect(VISIBILITY_REPORT.refreshAfterMs).toBeLessThanOrEqual(VISIBILITY_REPORT.pingMs);
  });

  it('a hidden page says so at once and sends nothing by the timer', () => {
    const sent: boolean[] = [];
    const h = host();
    const stop = trackVisibility(
      createVisibilityReporter((v) => sent.push(v)),
      h,
    );
    h.state.visible = false;
    h.document.dispatchEvent(new Event('visibilitychange'));
    expect(sent).toEqual([true, false]);
    vi.advanceTimersByTime(30 * 60_000);
    expect(sent).toEqual([true, false]);
    h.state.visible = true;
    h.document.dispatchEvent(new Event('visibilitychange'));
    vi.advanceTimersByTime(VISIBILITY_REPORT.settleMs);
    expect(sent).toEqual([true, false, true]);
    stop();
  });

  it('focus hops settle into nothing when the person is still looking', () => {
    const sent: boolean[] = [];
    const h = host();
    const stop = trackVisibility(
      createVisibilityReporter((v) => sent.push(v)),
      h,
    );
    h.window.dispatchEvent(new Event('blur'));
    h.window.dispatchEvent(new Event('focus'));
    vi.advanceTimersByTime(VISIBILITY_REPORT.settleMs);
    expect(sent).toEqual([true]);
    stop();
  });
});
