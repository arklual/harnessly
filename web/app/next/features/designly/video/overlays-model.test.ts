import { describe, expect, it } from 'vitest';
import {
  DRAG,
  STRIP,
  dragPlace,
  kindPatch,
  stripLanes,
  newCaptionPatch,
  overlayWire,
  placeOf,
  placePatch,
  stripSpan,
  visibleAt,
  withPlace,
} from '@/next/features/designly/video/overlays-model';
import type { IVideoOverlay } from '@/next/features/designly/video/types';

const overlay = (patch: Partial<IVideoOverlay> = {}): IVideoOverlay => ({
  id: 'caption-1',
  kind: 'caption',
  text: 'Привет',
  at: 1000,
  out: 3000,
  x: 0.06,
  y: 0.76,
  w: 0.5,
  h: 0.1,
  tx: null,
  ty: null,
  portrait: null,
  anchor: 'left',
  enter: 'mask',
  leave: 'fade',
  theme: 'auto',
  value: null,
  from: 0,
  decimals: 0,
  author: 'agent',
  updatedAt: '',
  ...patch,
});

describe('placeOf', () => {
  it('16:9 is the base place', () => {
    expect(placeOf(overlay(), '16:9')).toMatchObject({ x: 0.06, y: 0.76, w: 0.5, h: 0.1 });
  });
  it('9:16 moves the base place into the safe zone', () => {
    const p = placeOf(overlay({ y: 0.95, h: 0.05 }), '9:16');
    expect(p.y + p.h).toBeLessThanOrEqual(1 - 0.17 + 1e-9);
  });
  it('9:16 takes its own place', () => {
    const own = { x: 0.1, y: 0.2, w: 0.3, h: 0.1, tx: null, ty: null };
    expect(placeOf(overlay({ portrait: own }), '9:16')).toEqual(own);
  });
  it('a place laid into 9:16 goes to portrait, into 16:9 to the base', () => {
    const p = { x: 0.2, y: 0.3, w: 0.4, h: 0.1, tx: null, ty: null };
    expect(withPlace(overlay(), '9:16', p).portrait).toEqual(p);
    expect(withPlace(overlay(), '16:9', p).x).toBe(0.2);
    expect(placePatch('9:16', p)).toEqual({ portrait: { x: 0.2, y: 0.3, w: 0.4, h: 0.1 } });
    expect(placePatch('16:9', p)).toEqual({ x: 0.2, y: 0.3, w: 0.4, h: 0.1 });
  });
});

describe('visibleAt', () => {
  it('is on from at to out, to the scene end for a null out', () => {
    expect(visibleAt(overlay(), 999, 5000)).toBe(false);
    expect(visibleAt(overlay(), 1000, 5000)).toBe(true);
    expect(visibleAt(overlay(), 3000, 5000)).toBe(false);
    expect(visibleAt(overlay({ out: null }), 4999, 5000)).toBe(true);
  });
});

describe('dragPlace', () => {
  const from = placeOf(overlay(), '16:9');
  it('moves and stays inside the frame', () => {
    const p = dragPlace(from, DRAG.move, 2, 2, '16:9');
    expect(p.x).toBeCloseTo(0.5);
    expect(p.y).toBeCloseTo(0.9);
  });
  it('snaps the middle of the box to the middle of the frame', () => {
    const p = dragPlace(from, DRAG.move, 0.25 - 0.06 + 0.005, 0, '16:9');
    expect(p.x + p.w / 2).toBeCloseTo(0.5);
  });
  it('does not snap far from a mark', () => {
    const p = dragPlace(from, DRAG.move, 0.1, 0, '16:9');
    expect(p.x).toBeCloseTo(0.16);
  });
  it('resizes inside the frame and not below the minimum', () => {
    expect(dragPlace(from, DRAG.resize, 5, 5, '16:9')).toMatchObject({ w: 0.94, h: 0.24 });
    expect(dragPlace(from, DRAG.resize, -5, -5, '16:9')).toMatchObject({ w: 0.04, h: 0.04 });
  });
  it('carries the arrow point with a move and moves it alone by the target', () => {
    const callout = { ...from, tx: 0.5, ty: 0.45 };
    const moved = dragPlace(callout, DRAG.move, 0.1, 0, '16:9');
    expect(moved.tx).toBeCloseTo(0.5 + (moved.x - callout.x));
    expect(dragPlace(callout, DRAG.target, 0.1, -0.1, '16:9')).toMatchObject({
      x: from.x,
      tx: 0.6,
      ty: 0.35,
    });
    expect(dragPlace(callout, DRAG.target, 9, 9, '16:9')).toMatchObject({ tx: 1, ty: 1 });
  });
});

describe('stripSpan', () => {
  it('moves the whole strip and keeps its length', () => {
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.move, 500, 10000)).toEqual({ at: 1500, out: 3500 });
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.move, -5000, 10000)).toEqual({ at: 0, out: 2000 });
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.move, 99999, 10000)).toEqual({ at: 8000, out: 10000 });
  });
  it('keeps a strip to the end open', () => {
    expect(stripSpan({ at: 1000, out: null }, STRIP.move, -500, 10000)).toEqual({ at: 500, out: 9500 });
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.end, 99999, 10000)).toEqual({ at: 1000, out: null });
  });
  it('drags the edges and never leaves less than 100 ms', () => {
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.start, 99999, 10000)).toEqual({ at: 2900, out: 3000 });
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.end, -99999, 10000)).toEqual({ at: 1000, out: 1100 });
    expect(stripSpan({ at: 1000, out: 3000 }, STRIP.start, -99999, 10000)).toEqual({ at: 0, out: 3000 });
  });
});

describe('the words to the runtime and the server', () => {
  it('leaves absent optionals out', () => {
    const w = overlayWire(overlay());
    expect(w).not.toHaveProperty('tx');
    expect(w).not.toHaveProperty('portrait');
    expect(w).not.toHaveProperty('value');
    expect(w).toMatchObject({ id: 'caption-1', out: 3000 });
  });
  it('a new caption lasts 3 s, or to the end of a short scene', () => {
    expect(newCaptionPatch(2004, 10000, 'a')).toEqual({ kind: 'caption', text: 'a', at: 2000, out: 5000 });
    expect(newCaptionPatch(9900, 10000, 'a')).toMatchObject({ at: 9000, out: null });
  });
});

describe('stripLanes', () => {
  it('puts the ones that overlap in time on lanes of their own', () => {
    const a = overlay({ id: 'a', at: 0, out: 2000 });
    const b = overlay({ id: 'b', at: 1000, out: 3000 });
    const c = overlay({ id: 'c', at: 2000, out: null });
    const lanes = stripLanes([a, b, c], 5000);
    expect([lanes.get('a'), lanes.get('b'), lanes.get('c')]).toEqual([0, 1, 0]);
  });
});

describe('kindPatch', () => {
  it('gives a callout its arrow point and a counter its number', () => {
    expect(kindPatch(overlay(), 'callout', '16:9')).toMatchObject({
      kind: 'callout',
      tx: expect.any(Number),
    });
    expect(kindPatch(overlay(), 'counter', '16:9')).toMatchObject({ kind: 'counter', value: 100 });
    expect(kindPatch(overlay(), 'title', '16:9')).toEqual({ kind: 'title' });
  });
});
