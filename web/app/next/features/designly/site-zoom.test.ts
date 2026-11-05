import { describe, expect, it } from 'vitest';
import { DEVICE_GROUP, VP_KIND } from '@/next/features/designly/devices';
import {
  deltaOfScale,
  FIT,
  fitView,
  takesPagePan,
  isFit,
  keptInView,
  layoutCorner,
  panBy,
  shiftOf,
  standardFit,
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
  /** Where the box stands on the area with a view: its corner as the layout put it moved by the view, and its size. */
  const onArea = (
    z: { s: number; x: number; y: number },
    corner: { x: number; y: number },
    box: { w: number; h: number },
  ) => ({
    l: corner.x + z.x,
    t: corner.y + z.y,
    w: box.w * z.s,
    h: box.h * z.s,
  });

  it('sets the box whole in the middle of the padded inside, with the same room on either side', () => {
    const pad = { l: 24, t: 24, r: 24, b: 88 };
    const area = { w: 748, h: 712 };
    // Inside: 700 × 600; a phone 300 × 500 at the layout's fit stands in its middle, at (224, 74).
    const box = { w: 300, h: 500 };
    expect(layoutCorner(box, area, pad)).toEqual({ x: 224, y: 74 });
    expect(fitView(FIT.auto, box, area, pad)).toEqual(ZOOM_FIT);
    const whole = fitView(FIT.whole, box, area, pad);
    expect(whole.s).toBeCloseTo(600 / 500);
    const at = onArea(whole, layoutCorner(box, area, pad), box);
    // By its height: from the inside's top to its bottom, and the same room left and right.
    expect(at.t).toBeCloseTo(24);
    expect(at.t + at.h).toBeCloseTo(712 - 88);
    expect(at.l - 24).toBeCloseTo(748 - 24 - (at.l + at.w));
    // A strip lying is fitted by its width, in the middle of the height.
    const strip = { w: 600, h: 100 };
    const lying = onArea(fitView(FIT.whole, strip, area, pad), layoutCorner(strip, area, pad), strip);
    expect(lying.l).toBeCloseTo(24);
    expect(lying.w).toBeCloseTo(700);
    expect(lying.t - 24).toBeCloseTo(712 - 88 - (lying.t + lying.h));
  });

  it('centres a box the layout could not: taller than the inside, it starts at the top (HRN-674)', () => {
    // The island keeps 96 px under the page while the layout's fit kept 24: the phone is taller than the inside, the
    // layout puts it at the inside's top (safe centre), and the fit must count from there, not from a middle.
    const pad = { l: 24, t: 24, r: 24, b: 96 };
    const area = { w: 1414, h: 784 };
    const box = { w: 363, h: 736 };
    const corner = layoutCorner(box, area, pad);
    expect(corner.y).toBe(24);
    const at = onArea(fitView(FIT.whole, box, area, pad), corner, box);
    expect(at.t).toBeCloseTo(24);
    expect(at.t + at.h).toBeCloseTo(784 - 96);
    expect(at.l - 24).toBeCloseTo(1414 - 24 - (at.l + at.w));
  });

  it('is the same wherever the view was: a fit does not follow a zoom or a pan', () => {
    const pad = { l: 24, t: 24, r: 24, b: 24 };
    const area = { w: 1000, h: 800 };
    const box = { w: 400, h: 600 };
    const fitted = fitView(FIT.whole, box, area, pad);
    const moved = panBy(zoomAt(fitted, { x: 390, y: 590 }, 0.54), 300, 200);
    expect(moved).not.toEqual(fitted);
    expect(fitView(FIT.whole, box, area, pad)).toEqual(fitted);
  });

  it('never makes a computer larger than its own size, a phone may be', () => {
    const pad = { l: 0, t: 0, r: 0, b: 0 };
    const area = { w: 3000, h: 2000 };
    // At the layout's fit 1, a page of 1280 × 800 fitted whole would be as wide as the area, 2.34 times its size.
    const page = { w: 1280, h: 800 };
    expect(fitView(FIT.whole, page, area, pad).s).toBeCloseTo(3000 / 1280);
    const shrink = fitView(FIT.shrink, page, area, pad);
    expect(shrink.s).toBe(1);
    const at = onArea(shrink, layoutCorner(page, area, pad), page);
    expect(at.l).toBeCloseTo((3000 - 1280) / 2);
    expect(at.t).toBeCloseTo((2000 - 800) / 2);
    // A layout's fit of 0.5 lets the view double it, no more.
    expect(fitView(FIT.shrink, { w: 640, h: 400 }, area, pad, 0.5).s).toBe(2);
    // Larger than the canvas goes, a fit stops at its most.
    expect(fitView(FIT.whole, { w: 30, h: 20 }, area, pad).s).toBe(ZOOM.max);
  });

  it('picks the standard view by the device', () => {
    expect(standardFit(VP_KIND.fill, DEVICE_GROUP.view)).toBe(FIT.auto);
    expect(standardFit(VP_KIND.overview, DEVICE_GROUP.view)).toBe(FIT.auto);
    expect(standardFit(VP_KIND.frame, DEVICE_GROUP.phones)).toBe(FIT.whole);
    expect(standardFit(VP_KIND.size, DEVICE_GROUP.phones)).toBe(FIT.whole);
    expect(standardFit(VP_KIND.frame, DEVICE_GROUP.tablets)).toBe(FIT.whole);
    expect(standardFit(VP_KIND.size, DEVICE_GROUP.computers)).toBe(FIT.shrink);
    // «По окну» at a size of its own.
    expect(standardFit(VP_KIND.size, DEVICE_GROUP.view)).toBe(FIT.shrink);
  });

  it('takes a pan from the page unless locked, or a wheel over a page that fills the area at its fit', () => {
    expect(takesPagePan({ locked: false, drag: false, fill: false, atFit: true })).toBe(true);
    expect(takesPagePan({ locked: false, drag: false, fill: true, atFit: true })).toBe(false);
    expect(takesPagePan({ locked: false, drag: false, fill: true, atFit: false })).toBe(true);
    expect(takesPagePan({ locked: false, drag: true, fill: true, atFit: true })).toBe(true);
    expect(takesPagePan({ locked: true, drag: true, fill: false, atFit: false })).toBe(false);
  });
});
