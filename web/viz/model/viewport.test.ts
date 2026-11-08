// @vitest-environment jsdom
// SPDX-License-Identifier: Apache-2.0
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { HOUR } from './time';
import { VizViewport, type VizViewportOptions } from './viewport';

const EXTENT = { start: 0, end: 100 * HOUR };

describe('VizViewport', () => {
  let viewports: VizViewport[] = [];
  let frames: FrameRequestCallback[] = [];

  beforeEach(() => {
    frames = [];
    // Кадры анимации крутим руками: так тест не зависит от часов.
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => frames.push(cb));
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation(() => undefined);
  });

  afterEach(() => {
    viewports.forEach((v) => v.dispose());
    viewports = [];
  });

  function make(options: Partial<VizViewportOptions> = {}): VizViewport {
    const viewport = new VizViewport({ extent: EXTENT, ...options });
    viewports.push(viewport);
    return viewport;
  }

  /** Прогоняет кадры, пока анимация не закончится; возвращает их число. */
  function runFrames(limit = 2000): number {
    let now = performance.now();
    let count = 0;
    while (frames.length && count < limit) {
      const cb = frames.shift()!;
      now += 16;
      cb(now);
      count++;
    }
    return count;
  }

  describe('initial state', () => {
    it('shows the whole history by default', () => {
      const v = make();
      expect(v.state.current).toEqual(EXTENT);
      expect(v.state.target).toEqual(EXTENT);
      expect(v.state.animating).toBe(false);
      expect(v.extent).toEqual(EXTENT);
    });

    it('starts at the initial window', () => {
      const v = make({ initial: { start: 10 * HOUR, end: 30 * HOUR } });
      expect(v.state.current).toEqual({ start: 10 * HOUR, end: 30 * HOUR });
    });

    it('clamps the initial window to the extent', () => {
      const v = make({ initial: { start: -50 * HOUR, end: 10 * HOUR } });
      expect(v.state.current).toEqual({ start: 0, end: 60 * HOUR });
    });
  });

  describe('clamping', () => {
    it('shifts a window that sticks out of the left edge', () => {
      const v = make();
      v.setRange({ start: -10 * HOUR, end: 20 * HOUR }, { instant: true });
      expect(v.state.target).toEqual({ start: 0, end: 30 * HOUR });
    });

    it('shifts a window that sticks out of the right edge', () => {
      const v = make();
      v.setRange({ start: 90 * HOUR, end: 120 * HOUR }, { instant: true });
      expect(v.state.target).toEqual({ start: 70 * HOUR, end: 100 * HOUR });
    });

    it('widens a window narrower than minSpan (3 h by default) around its centre', () => {
      const v = make();
      v.setRange({ start: 50 * HOUR, end: 51 * HOUR }, { instant: true });
      const { start, end } = v.state.target;
      expect(end - start).toBe(3 * HOUR);
      expect((start + end) / 2).toBe(50.5 * HOUR);
    });

    it('honours a custom minSpan', () => {
      const v = make({ minSpan: 10 * HOUR });
      v.setRange({ start: 50 * HOUR, end: 51 * HOUR }, { instant: true });
      expect(v.state.target.end - v.state.target.start).toBe(10 * HOUR);
    });

    it('limits a window wider than the extent to the extent', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR } });
      v.setRange({ start: -500 * HOUR, end: 500 * HOUR }, { instant: true });
      expect(v.state.target).toEqual(EXTENT);
    });

    it('keeps a window inside a tiny extent even if minSpan is larger', () => {
      const v = make({ extent: { start: 0, end: HOUR } });
      expect(v.state.target).toEqual({ start: 0, end: HOUR });
    });

    it('re-clamps the window when the extent shrinks', () => {
      const v = make();
      v.setExtent({ start: 0, end: 50 * HOUR });
      expect(v.extent).toEqual({ start: 0, end: 50 * HOUR });
      expect(v.state.target).toEqual({ start: 0, end: 50 * HOUR });
      expect(v.state.current.end).toBeLessThanOrEqual(50 * HOUR);
    });
  });

  describe('zoomAround', () => {
    const initial = { start: 20 * HOUR, end: 80 * HOUR };

    it('keeps the anchor fixed at the same fraction of the window', () => {
      const v = make({ initial });
      const anchor = 35 * HOUR;
      const before = (anchor - initial.start) / (initial.end - initial.start);
      v.zoomAround(anchor, 0.5, { instant: true });
      const { start, end } = v.state.target;
      expect(end - start).toBe(30 * HOUR);
      expect((anchor - start) / (end - start)).toBeCloseTo(before, 10);
    });

    it('zooms around the centre symmetrically', () => {
      const v = make({ initial });
      v.zoomAround(50 * HOUR, 0.5, { instant: true });
      expect(v.state.target).toEqual({ start: 35 * HOUR, end: 65 * HOUR });
    });

    it('zooms out, but not beyond the extent', () => {
      const v = make({ initial });
      v.zoomAround(50 * HOUR, 10, { instant: true });
      expect(v.state.target).toEqual(EXTENT);
    });

    it('does not zoom in below minSpan', () => {
      const v = make({ initial });
      for (let i = 0; i < 20; i++) v.zoomAround(50 * HOUR, 0.3, { instant: true });
      expect(v.state.target.end - v.state.target.start).toBe(3 * HOUR);
    });

    it('compounds from the target, not from the animated window', () => {
      const v = make({ initial });
      v.zoomAround(50 * HOUR, 0.5);
      v.zoomAround(50 * HOUR, 0.5);
      expect(v.state.target.end - v.state.target.start).toBe(15 * HOUR);
    });
  });

  describe('panBy', () => {
    const initial = { start: 20 * HOUR, end: 80 * HOUR };

    it('shifts the window keeping its width', () => {
      const v = make({ initial });
      v.panBy(10 * HOUR, { instant: true });
      expect(v.state.target).toEqual({ start: 30 * HOUR, end: 90 * HOUR });
    });

    it('stops at the right edge', () => {
      const v = make({ initial });
      v.panBy(500 * HOUR, { instant: true });
      expect(v.state.target).toEqual({ start: 40 * HOUR, end: 100 * HOUR });
    });

    it('stops at the left edge', () => {
      const v = make({ initial });
      v.panBy(-500 * HOUR, { instant: true });
      expect(v.state.target).toEqual({ start: 0, end: 60 * HOUR });
    });
  });

  describe('setRange', () => {
    it('instant: current jumps to target and subscribers are notified once', () => {
      const v = make();
      const listener = vi.fn();
      v.subscribe(listener);
      v.setRange({ start: 10 * HOUR, end: 20 * HOUR }, { instant: true });
      expect(v.state.current).toEqual(v.state.target);
      expect(v.state.animating).toBe(false);
      expect(listener).toHaveBeenCalledTimes(1);
      expect(listener.mock.calls[0][0].current).toEqual({ start: 10 * HOUR, end: 20 * HOUR });
      expect(frames).toHaveLength(0);
    });

    it('animated: moves only the target and requests a frame', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR } });
      v.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      expect(v.state.target).toEqual({ start: 50 * HOUR, end: 70 * HOUR });
      expect(v.state.current).toEqual({ start: 20 * HOUR, end: 40 * HOUR });
      expect(v.state.animating).toBe(true);
      expect(frames).toHaveLength(1);
    });

    it('does not stack frame requests', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR } });
      v.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      v.setRange({ start: 55 * HOUR, end: 75 * HOUR });
      expect(frames).toHaveLength(1);
    });

    it('with tau = 0 jumps without animation', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR }, tau: 0 });
      v.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      expect(v.state.current).toEqual({ start: 50 * HOUR, end: 70 * HOUR });
      expect(frames).toHaveLength(0);
    });

    it('setTau(0) switches animation off at runtime', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR } });
      v.setTau(0);
      v.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      expect(v.state.current).toEqual({ start: 50 * HOUR, end: 70 * HOUR });
    });
  });

  describe('step', () => {
    const initial = { start: 20 * HOUR, end: 40 * HOUR };

    it('moves current towards target without overshooting', () => {
      const v = make({ initial });
      v.setRange({ start: 50 * HOUR, end: 90 * HOUR });
      const moving = v.step(16);
      const { start, end } = v.state.current;
      expect(moving).toBe(true);
      expect(start).toBeGreaterThan(20 * HOUR);
      expect(start).toBeLessThan(50 * HOUR);
      expect(end).toBeGreaterThan(40 * HOUR);
      expect(end).toBeLessThan(90 * HOUR);
    });

    it('converges to the target and reports completion', () => {
      const v = make({ initial });
      v.setRange({ start: 50 * HOUR, end: 90 * HOUR });
      let steps = 0;
      while (v.step(16) && steps < 5000) steps++;
      expect(steps).toBeLessThan(5000);
      expect(v.state.current).toEqual(v.state.target);
      expect(v.step(16)).toBe(false);
    });

    it('approaches monotonically', () => {
      const v = make({ initial });
      v.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      let prev = v.state.current.start;
      for (let i = 0; i < 30; i++) {
        v.step(16);
        expect(v.state.current.start).toBeGreaterThanOrEqual(prev);
        prev = v.state.current.start;
      }
    });

    it('moves faster with a bigger dt', () => {
      const a = make({ initial });
      const b = make({ initial });
      a.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      b.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      a.step(16);
      b.step(100);
      expect(b.state.current.start).toBeGreaterThan(a.state.current.start);
    });

    it('is a no-op when current already equals target', () => {
      const v = make({ initial });
      expect(v.step(16)).toBe(false);
      expect(v.state.current).toEqual(initial);
    });
  });

  describe('animation loop', () => {
    it('notifies subscribers each frame and ends settled', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR } });
      const listener = vi.fn();
      v.subscribe(listener);
      v.setRange({ start: 50 * HOUR, end: 90 * HOUR });
      const count = runFrames();
      expect(count).toBeGreaterThan(1);
      expect(listener).toHaveBeenCalledTimes(count);
      expect(v.state.current).toEqual({ start: 50 * HOUR, end: 90 * HOUR });
      expect(v.state.animating).toBe(false);
      expect(frames).toHaveLength(0);
    });
  });

  describe('subscribe', () => {
    it('stops notifying after unsubscribe', () => {
      const v = make();
      const listener = vi.fn();
      const unsubscribe = v.subscribe(listener);
      v.setRange({ start: 10 * HOUR, end: 20 * HOUR }, { instant: true });
      unsubscribe();
      v.setRange({ start: 30 * HOUR, end: 40 * HOUR }, { instant: true });
      expect(listener).toHaveBeenCalledTimes(1);
    });

    it('supports several listeners', () => {
      const v = make();
      const a = vi.fn();
      const b = vi.fn();
      v.subscribe(a);
      v.subscribe(b);
      v.panBy(HOUR, { instant: true });
      expect(a).toHaveBeenCalledTimes(1);
      expect(b).toHaveBeenCalledTimes(1);
    });
  });

  describe('dispose', () => {
    it('cancels the pending frame and drops listeners', () => {
      const v = make({ initial: { start: 20 * HOUR, end: 40 * HOUR } });
      const listener = vi.fn();
      v.subscribe(listener);
      v.setRange({ start: 50 * HOUR, end: 70 * HOUR });
      v.dispose();
      expect(window.cancelAnimationFrame).toHaveBeenCalled();
      expect(v.state.animating).toBe(false);
      v.setRange({ start: 10 * HOUR, end: 20 * HOUR }, { instant: true });
      expect(listener).not.toHaveBeenCalled();
    });
  });
});
