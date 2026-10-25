// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useCanvasZoom } from '@/next/features/designly/use-canvas-zoom';

class FakeObserver {
  observe() {}
  disconnect() {}
}

/** An area 800×600 with a box 400×300 in its middle; the layout is faked, jsdom has none. */
const setup = () => {
  const area = document.createElement('div');
  const box = document.createElement('div');
  box.className = 'hx-vpbox';
  area.append(box);
  const section = document.createElement('section');
  section.append(area);
  document.body.append(section);
  area.getBoundingClientRect = () => new DOMRect(0, 0, 800, 600);
  section.getBoundingClientRect = () => new DOMRect(0, 0, 800, 600);
  Object.defineProperties(box, {
    offsetLeft: { value: 200 },
    offsetTop: { value: 150 },
    offsetWidth: { value: 400 },
    offsetHeight: { value: 300 },
  });
  Object.defineProperties(area, { clientWidth: { value: 800 }, clientHeight: { value: 600 } });
  const view = renderHook(() =>
    useCanvasZoom({ current: area }, { current: section }, 1, true, 'device', { w: 400, h: 300 }),
  );
  return { area, view };
};

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', FakeObserver);
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    cb(0);
    return 1;
  });
});
afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

const wheel = (area: HTMLElement, init: WheelEventInit) => {
  const event = new WheelEvent('wheel', {
    bubbles: true,
    cancelable: true,
    clientX: 400,
    clientY: 300,
    ...init,
  });
  act(() => {
    area.dispatchEvent(event);
  });
  return event;
};

describe('useCanvasZoom', () => {
  it('zooms by a pinch (a wheel with ctrl), takes the event from the browser', () => {
    const { area, view } = setup();
    const event = wheel(area, { ctrlKey: true, deltaY: -10 });
    expect(event.defaultPrevented).toBe(true);
    expect(view.result.current.pct).toBeGreaterThan(100);
    expect(view.result.current.zoomed).toBe(true);
  });

  it('zooms by ⌘ + wheel and pans by the plain wheel', () => {
    const { area, view } = setup();
    wheel(area, { metaKey: true, deltaY: 10 });
    expect(view.result.current.pct).toBeLessThan(100);
    const before = area.style.getPropertyValue('--zx');
    wheel(area, { deltaX: -30, deltaY: 0 });
    expect(area.style.getPropertyValue('--zx')).not.toBe(before);
  });

  it('zooms by the keys and goes back to the fit by ⌘0', () => {
    const { area, view } = setup();
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '=', metaKey: true, cancelable: true }));
    });
    expect(view.result.current.pct).toBe(125);
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '0', metaKey: true, cancelable: true }));
    });
    expect(view.result.current.pct).toBe(100);
    expect(view.result.current.zoomed).toBe(false);
    expect(area.dataset.zoomed).toBe('false');
  });

  it('resets the view, and sets a scale from the menu smoothly', () => {
    const { area, view } = setup();
    act(() => view.result.current.zoomTo(200));
    expect(view.result.current.pct).toBe(200);
    expect(area.dataset.zsmooth).toBe('true');
    act(() => view.result.current.reset());
    expect(view.result.current.pct).toBe(100);
    expect(view.result.current.zoomed).toBe(false);
  });

  it("takes Safari's pinch as a gesture", () => {
    const { area, view } = setup();
    const start = new Event('gesturestart', { cancelable: true });
    act(() => {
      area.dispatchEvent(start);
    });
    const change = Object.assign(new Event('gesturechange', { cancelable: true }), {
      scale: 1.5,
      clientX: 400,
      clientY: 300,
    });
    act(() => {
      area.dispatchEvent(change);
    });
    expect(change.defaultPrevented).toBe(true);
    expect(view.result.current.pct).toBe(150);
  });

  it('leaves the keys to a field being typed in', () => {
    const { view } = setup();
    const input = document.createElement('input');
    document.body.append(input);
    act(() => {
      input.dispatchEvent(
        new KeyboardEvent('keydown', { key: '=', metaKey: true, bubbles: true, cancelable: true }),
      );
    });
    expect(view.result.current.pct).toBe(100);
  });
  it('fits the box by width, height or whole, and a pan lets the fit go', () => {
    const { area, view } = setup();
    act(() => view.result.current.fitTo('width'));
    expect(view.result.current.fitMode).toBe('width');
    expect(view.result.current.pct).toBe(200);
    expect(area.style.getPropertyValue('--zx')).toBe('-200px');
    expect(area.dataset.zsmooth).toBe('true');
    act(() => view.result.current.fitTo('height'));
    expect(view.result.current.pct).toBe(200);
    wheel(area, { deltaX: 0, deltaY: 40 });
    expect(view.result.current.fitMode).toBeNull();
    act(() => view.result.current.reset());
    expect(view.result.current.fitMode).toBe('auto');
    expect(view.result.current.zoomed).toBe(false);
  });

  it('locked, neither pans nor zooms, but still fits', () => {
    const { area, view } = setup();
    act(() => view.result.current.onLock());
    expect(view.result.current.locked).toBe(true);
    expect(area.dataset.locked).toBe('true');
    expect(view.result.current.canIn).toBe(false);
    const event = wheel(area, { ctrlKey: true, deltaY: -10 });
    expect(event.defaultPrevented).toBe(true);
    wheel(area, { deltaX: -30, deltaY: 0 });
    act(() => view.result.current.zoomIn());
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: '=', metaKey: true, cancelable: true }));
    });
    act(() => view.result.current.fromPage.pan(30, 30, true));
    expect(view.result.current.pct).toBe(100);
    expect(area.style.getPropertyValue('--zx')).toBe('0px');
    act(() => view.result.current.fitTo('whole'));
    expect(view.result.current.pct).toBe(200);
    act(() => view.result.current.onLock());
    expect(view.result.current.canIn).toBe(true);
  });

  it("takes Space and the pans the page's bridge tells", () => {
    const { area, view } = setup();
    act(() => view.result.current.fromPage.space(true));
    expect(area.dataset.space).toBe('true');
    act(() => view.result.current.fromPage.space(false));
    expect(area.dataset.space).toBe('false');
    act(() => view.result.current.fromPage.pan(-20, 10, false));
    expect(area.style.getPropertyValue('--zx')).toBe('-20px');
    expect(area.style.getPropertyValue('--zy')).toBe('10px');
  });

  it("leaves a page that fills the area at its fit to the page's wheel", () => {
    const { area, view } = setup();
    area.dataset.dev = 'fill';
    act(() => view.result.current.fromPage.pan(0, -40, false));
    expect(area.style.getPropertyValue('--zy')).toBe('0px');
    act(() => view.result.current.fromPage.pan(0, -40, true));
    expect(area.style.getPropertyValue('--zy')).toBe('-40px');
  });
  it('is a layer of its own while it moves, and drawn again at its scale once settled', () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      const { area } = setup();
      vi.advanceTimersByTime(1000);
      expect(area.dataset.zmoving).toBe('false');
      wheel(area, { deltaX: -30, deltaY: 0 });
      expect(area.dataset.zmoving).toBe('true');
      vi.advanceTimersByTime(100);
      wheel(area, { deltaX: -30, deltaY: 0 });
      vi.advanceTimersByTime(100);
      expect(area.dataset.zmoving).toBe('true');
      vi.advanceTimersByTime(60);
      expect(area.dataset.zmoving).toBe('false');
    } finally {
      vi.useRealTimers();
    }
  });
});
