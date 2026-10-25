// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HSCROLL_DIR, scrollTarget, syncEdges, useHScroll } from '@/next/ui/use-h-scroll';

const strip = (scrollLeft: number, clientWidth: number, scrollWidth: number) => {
  const el = document.createElement('div');
  Object.defineProperties(el, {
    scrollLeft: { value: scrollLeft, writable: true },
    clientWidth: { value: clientWidth },
    scrollWidth: { value: scrollWidth },
  });
  return el;
};

describe('syncEdges', () => {
  it('marks only the edge that has more behind it', () => {
    const el = strip(0, 300, 900);
    syncEdges(el);
    expect(el.hasAttribute('data-hs-l')).toBe(false);
    expect(el.hasAttribute('data-hs-r')).toBe(true);
    el.scrollLeft = 300;
    syncEdges(el);
    expect(el.hasAttribute('data-hs-l')).toBe(true);
    expect(el.hasAttribute('data-hs-r')).toBe(true);
    el.scrollLeft = 600;
    syncEdges(el);
    expect(el.hasAttribute('data-hs-r')).toBe(false);
  });

  it('marks nothing when the content fits', () => {
    const el = strip(0, 300, 300);
    syncEdges(el);
    expect(el.hasAttribute('data-hs-l') || el.hasAttribute('data-hs-r')).toBe(false);
  });
});

describe('scrollTarget', () => {
  const item = (left: number, width: number) => {
    const node = document.createElement('div');
    node.getBoundingClientRect = () => ({ left }) as DOMRect;
    Object.defineProperty(node, 'offsetWidth', { value: width });
    return node;
  };

  it('is null for an item in view and a position for the ones outside', () => {
    const el = strip(100, 300, 900);
    el.getBoundingClientRect = () => ({ left: 0 }) as DOMRect;
    expect(scrollTarget(el, item(150, 80))).toBeNull();
    // Left of the view: its left edge minus the gap.
    expect(scrollTarget(el, item(-50, 80))).toBe(100 - 50 - 24);
    // Right of the view: its right edge plus the gap lands on the strip's right edge.
    expect(scrollTarget(el, item(500, 80))).toBe(100 + 500 + 80 + 24 - 300);
  });
});

describe('useHScroll', () => {
  beforeEach(() => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        observe = vi.fn();
        disconnect = vi.fn();
      },
    );
    vi.stubGlobal('matchMedia', () => ({ matches: false }));
  });
  afterEach(() => vi.unstubAllGlobals());

  it('steps by 80% of the strip in the asked direction', () => {
    const el = strip(0, 300, 900);
    el.scrollBy = vi.fn();
    const { result } = renderHook(() => useHScroll({ current: el }));
    result.current(HSCROLL_DIR.next);
    result.current(HSCROLL_DIR.prev);
    expect(el.scrollBy).toHaveBeenNthCalledWith(1, { left: 240, behavior: 'smooth' });
    expect(el.scrollBy).toHaveBeenNthCalledWith(2, { left: -240, behavior: 'smooth' });
  });

  it('turns a wheel going down into a sideways move of a strip that has more behind it', () => {
    const el = strip(0, 300, 900);
    renderHook(() => useHScroll({ current: el }));
    const wheel = new WheelEvent('wheel', { deltaY: 120, cancelable: true });
    el.dispatchEvent(wheel);
    expect(el.scrollLeft).toBe(120);
    expect(wheel.defaultPrevented).toBe(true);
    el.dispatchEvent(new WheelEvent('wheel', { deltaY: 1000, cancelable: true }));
    expect(el.scrollLeft).toBe(600);
  });

  it('leaves a wheel alone where the strip fits, is at its end or already goes sideways', () => {
    const fits = strip(0, 300, 300);
    renderHook(() => useHScroll({ current: fits }));
    const onFit = new WheelEvent('wheel', { deltaY: 100, cancelable: true });
    fits.dispatchEvent(onFit);
    expect(onFit.defaultPrevented).toBe(false);
    const end = strip(600, 300, 900);
    renderHook(() => useHScroll({ current: end }));
    const atEnd = new WheelEvent('wheel', { deltaY: 100, cancelable: true });
    end.dispatchEvent(atEnd);
    expect(atEnd.defaultPrevented).toBe(false);
    const wide = strip(0, 300, 900);
    renderHook(() => useHScroll({ current: wide }));
    const sideways = new WheelEvent('wheel', { deltaX: 100, deltaY: 10, cancelable: true });
    wide.dispatchEvent(sideways);
    expect(sideways.defaultPrevented).toBe(false);
  });
});
