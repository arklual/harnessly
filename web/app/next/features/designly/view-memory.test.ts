// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { FIT, ZOOM, ZOOM_FIT } from '@/next/features/designly/site-zoom';
import {
  parseView,
  readView,
  startView,
  VIEW_DEFAULT,
  writeView,
  type IViewMemory,
} from '@/next/features/designly/view-memory';

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

const kept: IViewMemory = {
  device: 'ip15',
  size: null,
  rot: false,
  chrome: false,
  browser: 'ios18',
  fit: null,
  zoom: { s: 2, x: -30, y: 12 },
  locked: true,
  box: { w: 400, h: 800 },
  page: 'pages/pricing',
};

describe('the kept view', () => {
  it('comes back as it was written', () => {
    writeView('d1', kept);
    expect(readView('d1')).toEqual(kept);
  });

  it('is kept per design', () => {
    writeView('d1', kept);
    writeView('d2', { ...kept, device: 'fill', chrome: true });
    expect(readView('d1').device).toBe('ip15');
    expect(readView('d2').device).toBe('fill');
    expect(readView('d3')).toEqual(VIEW_DEFAULT);
  });

  it('keeps a size of «Резина» only on the window itself', () => {
    expect(parseView({ v: 1, device: 'fill', size: { w: 1338, h: 1288 } }).size).toEqual({
      w: 1338,
      h: 1288,
    });
    expect(parseView({ v: 1, device: 'ip15', size: { w: 1338, h: 1288 } }).size).toBeNull();
  });
});

describe('what is not valid', () => {
  it('goes back to the default for an unknown device, keeping the page', () => {
    const view = parseView({ ...kept, v: 1, device: 'no-such', page: 'a' });
    expect(view.device).toBe('fill');
    expect(view.page).toBe('a');
    expect(view.locked).toBe(false);
  });

  it('clamps the zoom and drops a view that is not numbers', () => {
    expect(parseView({ ...kept, v: 1, zoom: { s: 99, x: 1, y: 2 } }).zoom.s).toBe(ZOOM.max);
    expect(parseView({ ...kept, v: 1, zoom: { s: 0.01, x: 1, y: 2 } }).zoom.s).toBe(ZOOM.min);
    expect(parseView({ ...kept, v: 1, zoom: { s: 'a', x: 1, y: 2 } }).zoom).toEqual(ZOOM_FIT);
  });

  it('falls back to the auto fit for a fit nobody knows', () => {
    expect(parseView({ ...kept, v: 1, fit: 'cover' }).fit).toBe(FIT.auto);
    expect(parseView({ ...kept, v: 1, fit: FIT.whole }).fit).toBe(FIT.whole);
  });

  it('ignores junk, another version and broken storage', () => {
    expect(parseView(null)).toEqual(VIEW_DEFAULT);
    expect(parseView('x')).toEqual(VIEW_DEFAULT);
    expect(parseView({ ...kept, v: 7 })).toEqual(VIEW_DEFAULT);
    localStorage.setItem('hx-next-dl-view:d1', '{oops');
    expect(readView('d1')).toEqual(VIEW_DEFAULT);
  });

  it('survives storage that throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(readView('d1')).toEqual(VIEW_DEFAULT);
    expect(() => writeView('d1', kept)).not.toThrow();
  });
});

describe('startView', () => {
  it('keeps a fit whatever the box is', () => {
    expect(startView({ ...kept, fit: FIT.width }, { w: 10, h: 10 })).toEqual({
      fit: FIT.width,
      zoom: ZOOM_FIT,
    });
  });

  it('keeps a free view over a box of about the same size', () => {
    expect(startView(kept, { w: 420, h: 780 })).toEqual({ fit: null, zoom: kept.zoom });
  });

  it('puts the fit back over a box that changed a lot or was not measured', () => {
    expect(startView(kept, { w: 900, h: 800 })).toEqual({ fit: FIT.auto, zoom: ZOOM_FIT });
    expect(startView({ ...kept, box: null }, { w: 400, h: 800 }).fit).toBe(FIT.auto);
  });
});
