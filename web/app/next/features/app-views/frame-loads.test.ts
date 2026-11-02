import { describe, expect, it, vi } from 'vitest';
import { watchLoads } from '@/next/features/app-views/frame-loads';

describe('watchLoads: a frame that loads a second document has navigated', () => {
  it('lets the first load be and reports every later one', () => {
    const frame = new EventTarget();
    const navigated = vi.fn();
    watchLoads(frame, navigated);
    frame.dispatchEvent(new Event('load'));
    expect(navigated).not.toHaveBeenCalled();
    frame.dispatchEvent(new Event('load'));
    expect(navigated).toHaveBeenCalledTimes(1);
    frame.dispatchEvent(new Event('load'));
    expect(navigated).toHaveBeenCalledTimes(2);
  });

  it('stops listening when stopped', () => {
    const frame = new EventTarget();
    const navigated = vi.fn();
    const stop = watchLoads(frame, navigated);
    frame.dispatchEvent(new Event('load'));
    stop();
    frame.dispatchEvent(new Event('load'));
    expect(navigated).not.toHaveBeenCalled();
  });
});
