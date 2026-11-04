import { describe, expect, it } from 'vitest';
import {
  BARS,
  browserOf,
  DEVICE_GROUP,
  deviceGroups,
  deviceOf,
  deviceSizeText,
  FILL_INSET,
  MIN_FIT,
  overviewBox,
  overviewOf,
  SCROLL_START,
  syncedScroll,
  trackScroll,
  viewportOf,
  viewportVars,
  VP_KIND,
} from '@/next/features/designly/devices';

const AREA = { w: 1172, h: 776 };

describe('viewportOf', () => {
  it('never draws a box at 0% in an area measured empty', () => {
    expect(viewportOf(deviceOf('ip15'), { w: 0, h: 0 }, null, false).fit).toBeGreaterThan(0);
    expect(viewportOf(deviceOf('fill'), { w: 600, h: 40 }, { w: 300, h: 900 }, false).fit).toBeGreaterThan(0);
  });

  it('«По окну» is the area with a margin of canvas round it', () => {
    expect(viewportOf(deviceOf('fill'), AREA, null, false)).toMatchObject({
      kind: VP_KIND.fill,
      vw: 1172 - 2 * FILL_INSET,
      vh: 776 - 2 * FILL_INSET,
      fit: 1,
    });
  });

  it('an own size of «По окну» is drawn at that size and scaled to fit', () => {
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

  it('an unknown device is «По окну»', () => {
    expect(deviceOf('nope').id).toBe('fill');
  });
});

describe('the device menu', () => {
  it('groups the devices: the view, phones, tablets, computers, keeping the ids', () => {
    expect(deviceGroups().map(({ group, devices }) => [group, devices.map((d) => d.id)])).toEqual([
      [DEVICE_GROUP.view, ['fill', 'overview']],
      [DEVICE_GROUP.phones, ['ip14', 'ip15', 'ip16', 'px7', 's24', 'bare']],
      [DEVICE_GROUP.tablets, ['ipad']],
      [DEVICE_GROUP.computers, ['lap', 'fhd']],
    ]);
  });

  it('shows the size alone on the right, and none for the view', () => {
    expect(deviceSizeText(deviceOf('ip15'))).toBe('393×852');
    expect(deviceSizeText(deviceOf('fhd'))).toBe('1920×1080');
    expect(deviceSizeText(deviceOf('fill'))).toBeNull();
    expect(deviceSizeText(deviceOf('overview'))).toBeNull();
  });
});

describe('«Все экраны»', () => {
  it('is the area itself and puts the screens in a row by group at one scale', () => {
    expect(viewportOf(deviceOf('overview'), AREA, null, false)).toMatchObject({
      kind: VP_KIND.overview,
      vw: 1172,
      vh: 776,
    });
    const cards = overviewOf(AREA, false);
    expect(cards.map((c) => c.device.id)).toEqual([
      'ip14',
      'ip15',
      'ip16',
      'px7',
      's24',
      'ipad',
      'lap',
      'fhd',
    ]);
    expect(new Set(cards.map((c) => c.scale)).size).toBe(1);
    // A wider gap stands before the first tablet and the first computer.
    expect(cards.filter((c) => c.lead).map((c) => c.device.id)).toEqual(['ipad', 'lap']);
  });

  it('shows only the groups chosen, larger when there are fewer', () => {
    const phones = overviewOf(AREA, false, [DEVICE_GROUP.phones]);
    expect(phones.map((c) => c.device.group)).toEqual(Array(5).fill(DEVICE_GROUP.phones));
    expect(phones.some((c) => c.lead)).toBe(false);
    expect(phones[0].scale).toBeGreaterThan(overviewOf(AREA, false)[0].scale);
    // Phones alone fit the width of the area.
    expect(overviewBox(phones, false).w).toBeLessThanOrEqual(AREA.w + 0.5);
    expect(overviewOf(AREA, false, [])).toEqual([]);
  });

  it('keeps many screens readable and lets the row run past the area to be panned', () => {
    const all = overviewOf(AREA, false);
    expect(all[0].scale).toBeGreaterThanOrEqual(0.35 - 1e-9);
    expect(overviewBox(all, false).w).toBeGreaterThan(AREA.w);
    // The row never grows taller than the area.
    expect(overviewBox(all, false).h).toBeLessThanOrEqual(AREA.h + 0.5);
  });

  it('leaves room under the field and never draws smaller than the least scale', () => {
    const wide = { w: 6000, h: 700 };
    expect(overviewOf(wide, true)[0].scale).toBeLessThan(overviewOf(wide, false)[0].scale);
    expect(overviewOf({ w: 200, h: 100 }, false)[0].scale).toBe(MIN_FIT);
  });

  it('scrolls the other screens to the same place in whole CSS pixels, never above the top', () => {
    expect(syncedScroll(240.4)).toBe(240);
    expect(syncedScroll(-12)).toBe(0);
    expect(syncedScroll(0)).toBe(0);
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
  it('is the drawn row: the screens, the gaps (wider between groups), the captions and the padding', () => {
    const cards = overviewOf({ w: 1400, h: 700 }, false);
    const box = overviewBox(cards, false);
    const screens = cards.reduce((t, c) => t + c.w * c.scale, 0);
    // Eight screens: five gaps within groups, two between them, the sides.
    expect(box.w).toBeCloseTo(48 + screens + 28 * 5 + 64 * 2);
    expect(overviewBox(cards, true).h - box.h).toBe(76);
    expect(overviewBox([], true)).toEqual({ w: 0, h: 0 });
  });
});
