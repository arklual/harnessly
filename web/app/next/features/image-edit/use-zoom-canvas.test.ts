import { describe, expect, it } from 'vitest';
import { canvasSize } from '@/next/features/image-edit/use-zoom-canvas';

describe('the drawing layer over a zoomed picture (HRN-1500)', () => {
  it('gets the pixels of the zoom, so the marks stay sharp', () => {
    expect(canvasSize(400, 100, 2, 1)).toEqual({ w: 800, h: 200 });
    expect(canvasSize(400, 100, 2, 3)).toEqual({ w: 2400, h: 600 });
  });

  it('is never smaller than its box, zoomed out', () => {
    expect(canvasSize(400, 100, 1, 0.5)).toEqual({ w: 400, h: 100 });
  });

  it('stops adding pixels where a phone would refuse the canvas', () => {
    const { w, h } = canvasSize(1300, 200, 3, 8);
    expect(w * h).toBeLessThanOrEqual(16_000_000);
    expect(Math.max(w, h)).toBeLessThanOrEqual(16_384);
    expect(w / h).toBeCloseTo(6.5, 1);
  });
});
