// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ZOOM, ZOOM_FIT } from '@/next/features/designly/site-zoom';
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
  std: false,
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
    expect(view.locked).toBe(true);
  });

  it('locks a view kept before the lock was the default, keeps an unlock made since (HRN-673)', () => {
    expect(parseView({ v: 1, device: 'fill', locked: false }).locked).toBe(true);
    expect(parseView({ v: 2, device: 'fill', locked: false }).locked).toBe(false);
    expect(parseView({ v: 2, device: 'fill' }).locked).toBe(true);
  });

  it('drops a kept size under the least of «Резина»', () => {
    expect(parseView({ v: 1, device: 'fill', size: { w: 150, h: 932 } }).size).toBeNull();
    expect(parseView({ v: 1, device: 'fill', size: { w: 200, h: 200 } }).size).toEqual({ w: 200, h: 200 });
  });

  it('clamps the zoom and drops a view that is not numbers', () => {
    expect(parseView({ ...kept, v: 1, zoom: { s: 99, x: 1, y: 2 } }).zoom.s).toBe(ZOOM.max);
    expect(parseView({ ...kept, v: 1, zoom: { s: 0.01, x: 1, y: 2 } }).zoom.s).toBe(ZOOM.min);
    expect(parseView({ ...kept, v: 1, zoom: { s: 'a', x: 1, y: 2 } }).zoom).toEqual(ZOOM_FIT);
  });

  it('takes any fit of a record from before «Вписать» as the standard view, a moved one as moved', () => {
    const { std: _std, ...old } = kept;
    for (const fit of ['auto', 'width', 'height', 'whole', 'cover'])
      expect(parseView({ ...old, v: 2, fit }).std).toBe(true);
    expect(parseView({ ...old, v: 2, fit: null }).std).toBe(false);
    expect(parseView({ ...old, v: 2 }).std).toBe(true);
    expect(parseView({ ...kept, v: 2, std: true }).std).toBe(true);
  });

  it('turns only a phone or a tablet', () => {
    expect(parseView({ ...kept, v: 2, rot: true }).rot).toBe(true);
    expect(parseView({ ...kept, v: 2, device: 'ipad', rot: true }).rot).toBe(true);
    expect(parseView({ ...kept, v: 2, device: 'lap', rot: true }).rot).toBe(false);
    expect(parseView({ ...kept, v: 2, device: 'fill', rot: true }).rot).toBe(false);
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
  it('keeps the standard view whatever the box is', () => {
    expect(startView({ ...kept, std: true }, { w: 10, h: 10 })).toEqual({ std: true, zoom: ZOOM_FIT });
  });

  it('keeps a free view over a box of about the same size', () => {
    expect(startView(kept, { w: 420, h: 780 })).toEqual({ std: false, zoom: kept.zoom });
  });

  it('puts the standard view back over a box that changed a lot or was not measured', () => {
    expect(startView(kept, { w: 900, h: 800 })).toEqual({ std: true, zoom: ZOOM_FIT });
    expect(startView({ ...kept, box: null }, { w: 400, h: 800 }).std).toBe(true);
  });
});
