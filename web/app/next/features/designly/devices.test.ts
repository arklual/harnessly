import { describe, expect, it } from 'vitest';
import {
  BARS,
  browserOf,
  deviceOf,
  FILL_INSET,
  overviewBox,
  overviewOf,
  SCROLL_START,
  trackScroll,
  viewportOf,
  viewportVars,
  VP_KIND,
} from '@/next/features/designly/devices';

const AREA = { w: 1172, h: 776 };

describe('viewportOf', () => {
  it('«Резина» is the area with a margin of canvas round it', () => {
    expect(viewportOf(deviceOf('fill'), AREA, null, false)).toMatchObject({
      kind: VP_KIND.fill,
      vw: 1172 - 2 * FILL_INSET,
      vh: 776 - 2 * FILL_INSET,
      fit: 1,
    });
  });

  it('an own size of «Резина» is drawn at that size and scaled to fit', () => {
    const vp = viewportOf(deviceOf('fill'), AREA, { w: 1920, h: 1080 }, false);
    expect(vp).toMatchObject({ kind: VP_KIND.size, vw: 1920, vh: 1080 });
    expect(vp.fit).toBeCloseTo((1172 - 48) / 1920, 4);
  });

  it('a phone in its case scales the page into the case screen', () => {
    const vp = viewportOf(deviceOf('ip15'), AREA, null, false);
    expect(vp.kind).toBe(VP_KIND.frame);
    expect(vp.k).toBeCloseTo(390 / 393, 4);
    expect(vp.cvh).toBe(Math.round(830 / (390 / 393)));
    expect(vp.fit).toBeCloseTo((776 - 48) / 868, 4);
  });

  it('a turned device swaps the viewport and the case', () => {
    const vp = viewportOf(deviceOf('ipad'), AREA, null, true);
    expect(vp).toMatchObject({ rot: true, vw: 1194, vh: 834, bw: 778, bh: 560 });
  });

  it('an unknown device is «Резина»', () => {
    expect(deviceOf('nope').id).toBe('fill');
  });
});

describe('«Обзор»', () => {
  it('is the area itself and puts the devices side by side at one scale', () => {
    expect(viewportOf(deviceOf('overview'), AREA, null, false)).toMatchObject({
      kind: VP_KIND.overview,
      vw: 1172,
      vh: 776,
    });
    const cards = overviewOf(AREA, false);
    expect(cards.map((c) => c.device.id)).toEqual(['ip15', 'px7', 'ipad', 'lap']);
    expect(new Set(cards.map((c) => c.scale)).size).toBe(1);
    const [first] = cards;
    expect(first.scale).toBeLessThanOrEqual(1);
    // All of them fit the width of the area.
    const across = cards.reduce((sum, c) => sum + c.w * c.scale, 0) + 28 * 3 + 48;
    expect(across).toBeLessThanOrEqual(AREA.w + 0.5);
  });

  it('leaves room under the field and never draws smaller than a fifth', () => {
    const wide = { w: 3000, h: 700 };
    expect(overviewOf(wide, true)[0].scale).toBeLessThan(overviewOf(wide, false)[0].scale);
    expect(overviewOf({ w: 200, h: 100 }, false)[0].scale).toBe(0.2);
  });
});

describe('the phone interface', () => {
  it('a device in a case has a status bar and its system; a bare size has none', () => {
    expect(deviceOf('ip15')).toMatchObject({ os: 'ios', sb: 54 });
    expect(deviceOf('px7')).toMatchObject({ os: 'android', browser: 'chrome', sb: 36 });
    expect(deviceOf('lap').os).toBeUndefined();
    const vars = viewportVars(viewportOf(deviceOf('ip15'), AREA, null, false), deviceOf('ip15'));
    expect(vars['--sb']).toBe('54px');
  });

  it("the browser is the asked one of the device's system, else the device's own", () => {
    expect(browserOf(deviceOf('ip15'), null)?.id).toBe('ios26');
    expect(browserOf(deviceOf('ip15'), 'iostop')?.id).toBe('iostop');
    expect(browserOf(deviceOf('ip15'), 'chrome')?.id).toBe('ios26');
    expect(browserOf(deviceOf('s24'), null)?.id).toBe('sam85');
    expect(browserOf(deviceOf('lap'), null)).toBeNull();
  });

  it('the bars fold on a run down and unfold on a run up or at the top', () => {
    let track = trackScroll(SCROLL_START, 40);
    expect(track.bars).toBe(BARS.min);
    track = trackScroll(track, 44);
    expect(track.bars).toBe(BARS.min);
    // A short step back is a tremble.
    track = trackScroll(track, 40);
    expect(track.bars).toBe(BARS.min);
    track = trackScroll(track, 20);
    expect(track.bars).toBe(BARS.full);
    track = trackScroll(trackScroll(track, 300), 5);
    expect(track.bars).toBe(BARS.full);
  });
});

describe('overviewBox', () => {
  it('is the drawn row: the screens, the gaps, the labels and the padding, with room for the field', () => {
    const cards = overviewOf({ w: 1400, h: 700 }, false);
    const box = overviewBox(cards, false);
    const across = cards.reduce((t, c) => t + c.w * c.scale, 0);
    expect(box.w).toBeCloseTo(48 + across + 28 * (cards.length - 1));
    expect(overviewBox(cards, true).h - box.h).toBe(76);
    expect(overviewBox([], true)).toEqual({ w: 0, h: 0 });
  });
});
