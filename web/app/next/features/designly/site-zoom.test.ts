import { describe, expect, it } from 'vitest';
import {
  deltaOfScale,
  FIT,
  fitView,
  takesPagePan,
  isFit,
  keptInView,
  panBy,
  shiftOf,
  unzoomed,
  wheelFactor,
  ZOOM,
  ZOOM_FIT,
  ZOOM_KEY,
  zoomAt,
  zoomKeyOf,
  zoomPct,
} from '@/next/features/designly/site-zoom';

describe('site zoom', () => {
  it('keeps the point under the cursor in place', () => {
    const z = zoomAt(ZOOM_FIT, { x: 100, y: 50 }, 2);
    expect(z).toEqual({ s: 2, x: -100, y: -50 });
    // The point (100, 50) of the box is drawn at x + s·100 = 100: where it was.
    expect(z.x + z.s * 100).toBe(100);
    expect(z.y + z.s * 50).toBe(50);
  });

  it('holds the scale between its bounds', () => {
    expect(zoomAt(ZOOM_FIT, { x: 0, y: 0 }, 100).s).toBe(ZOOM.max);
    expect(zoomAt(ZOOM_FIT, { x: 0, y: 0 }, 0.01).s).toBe(ZOOM.min);
  });

  it('pans and says when it is at the fit', () => {
    expect(isFit(ZOOM_FIT)).toBe(true);
    expect(isFit(panBy(ZOOM_FIT, 3, 0))).toBe(false);
    expect(panBy({ s: 2, x: 1, y: 1 }, 4, -2)).toEqual({ s: 2, x: 5, y: -1 });
  });

  it('caps a wheel notch and follows a pinch', () => {
    expect(wheelFactor(-1000)).toBeCloseTo(Math.exp(0.2));
    expect(wheelFactor(1000)).toBeCloseTo(Math.exp(-0.2));
    expect(wheelFactor(-4)).toBeCloseTo(Math.exp(0.04));
  });

  it('reads the scale as the fit times the view', () => {
    expect(zoomPct(0.78, ZOOM_FIT)).toBe(78);
    expect(zoomPct(0.5, { s: 2, x: 0, y: 0 })).toBe(100);
  });

  it('carries a point of the section with the view and back', () => {
    const z = { s: 2, x: 30, y: -10 };
    const shift = shiftOf(z, { x: 100, y: 40 });
    const at = { x: 150, y: 90 };
    const shown = { x: z.s * at.x + shift.x, y: z.s * at.y + shift.y };
    expect(unzoomed(z, shift, shown)).toEqual(at);
    expect(shiftOf(ZOOM_FIT, { x: 100, y: 40 })).toEqual({ x: 0, y: 0 });
  });

  it('never lets the box leave the area', () => {
    const box = { x: 100, y: 50, w: 400, h: 800 };
    const area = { w: 600, h: 900 };
    expect(keptInView({ s: 1, x: 5000, y: 0 }, box, area).x).toBe(600 - 80 - 100);
    expect(keptInView({ s: 1, x: -5000, y: 0 }, box, area).x).toBe(80 - 100 - 400);
    expect(keptInView({ s: 1, x: 0, y: -5000 }, box, area).y).toBe(80 - 50 - 800);
    expect(keptInView({ s: 1, x: 10, y: 10 }, box, area)).toEqual({ s: 1, x: 10, y: 10 });
  });

  it('reads the zoom keys: ⌘/Ctrl with + − 0, and Shift+1', () => {
    const key = (k: string, extra: Partial<Parameters<typeof zoomKeyOf>[0]> = {}) =>
      zoomKeyOf({ key: k, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...extra });
    expect(key('=', { metaKey: true })).toBe(ZOOM_KEY.in);
    expect(key('+', { ctrlKey: true, shiftKey: true })).toBe(ZOOM_KEY.in);
    expect(key('-', { ctrlKey: true })).toBe(ZOOM_KEY.out);
    expect(key('0', { metaKey: true })).toBe(ZOOM_KEY.reset);
    expect(key('!', { shiftKey: true, code: 'Digit1' })).toBe(ZOOM_KEY.reset);
    expect(key('=')).toBeNull();
    expect(key('0')).toBeNull();
    expect(key('0', { metaKey: true, altKey: true })).toBeNull();
  });

  it('turns a gesture step back into the wheel delta the zoom reads', () => {
    expect(deltaOfScale(1)).toBeCloseTo(0);
    expect(wheelFactor(deltaOfScale(1.1))).toBeCloseTo(1.1);
    expect(wheelFactor(deltaOfScale(0.9))).toBeCloseTo(0.9);
    expect(deltaOfScale(0)).toBe(0);
  });
  it('fits the box by width, by height and whole, from where the layout put it', () => {
    const pad = { l: 24, t: 24, r: 24, b: 88 };
    const area = { w: 748, h: 712 };
    // Inside: 700 × 600; a phone 300 × 600 at the layout's fit stands at (224, 24).
    const box = { w: 300, h: 600 };
    expect(fitView(FIT.auto, box, area, pad)).toEqual(ZOOM_FIT);
    const width = fitView(FIT.width, box, area, pad);
    expect(width.s).toBeCloseTo(700 / 300);
    // Its left edge at the padding, its top at the top: taller than the area, its top shows.
    expect(224 + width.x).toBeCloseTo(24);
    expect(24 + width.y).toBeCloseTo(24);
    const height = fitView(FIT.height, box, area, pad);
    expect(height).toEqual({ s: 1, x: 0, y: 0 });
    const wide = { w: 700, h: 200 };
    const whole = fitView(FIT.whole, wide, area, pad);
    expect(whole.s).toBe(1);
    expect(fitView(FIT.height, wide, area, pad).s).toBe(3);
    // A strip fitted by its height would be larger than the canvas goes.
    expect(fitView(FIT.height, { w: 700, h: 100 }, area, pad).s).toBe(ZOOM.max);
  });

  it('takes a pan from the page unless locked, or a wheel over a page that fills the area at its fit', () => {
    expect(takesPagePan({ locked: false, drag: false, fill: false, atFit: true })).toBe(true);
    expect(takesPagePan({ locked: false, drag: false, fill: true, atFit: true })).toBe(false);
    expect(takesPagePan({ locked: false, drag: false, fill: true, atFit: false })).toBe(true);
    expect(takesPagePan({ locked: false, drag: true, fill: true, atFit: true })).toBe(true);
    expect(takesPagePan({ locked: true, drag: true, fill: false, atFit: false })).toBe(false);
  });
});
