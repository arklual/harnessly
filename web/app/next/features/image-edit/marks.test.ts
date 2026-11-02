import { describe, expect, it } from 'vitest';
import {
  LOOK,
  MAGENTA,
  MARK_KIND,
  NOTE_WIDTH,
  PEN_WIDTH,
  VIOLET,
  boxCounts,
  overlaySize,
  paintMarks,
  type IMarks,
  type TPaintContext,
} from '@/next/features/image-edit/marks';

/** A context that remembers what was drawn and in which colour. */
const recorder = () => {
  const calls: string[] = [];
  const ctx = {
    lineWidth: 0,
    strokeStyle: '' as string,
    lineCap: 'butt' as CanvasLineCap,
    lineJoin: 'miter' as CanvasLineJoin,
    clearRect: () => calls.push('clear'),
    beginPath: () => calls.push('begin'),
    moveTo: (x: number, y: number) => calls.push(`move ${x} ${y}`),
    lineTo: (x: number, y: number) => calls.push(`line ${x} ${y}`),
    stroke: () => calls.push(`stroke ${ctx.strokeStyle} ${ctx.lineWidth}`),
    strokeRect: (x: number, y: number, w: number, h: number) =>
      calls.push(`rect ${[x, y, w, h].map(Math.round).join(' ')} ${ctx.strokeStyle} ${ctx.lineWidth}`),
  };
  return { ctx: ctx as unknown as TPaintContext, calls };
};

const stroke = (color: string, width: number, ...points: [number, number][]) => ({
  color,
  width,
  points: points.map(([x, y]) => ({ x, y })),
});

describe('paintMarks', () => {
  it('writes erase strokes in opaque magenta whatever colour they carry', () => {
    const { ctx, calls } = recorder();
    const marks: IMarks = { strokes: [stroke('#ff3b5c', 0.04, [0.1, 0.2], [0.5, 0.2])], box: null };
    paintMarks(ctx, 1000, 500, MARK_KIND.erase, marks, LOOK.export);
    expect(calls).toEqual(['clear', 'begin', 'move 100 100', 'line 500 100', `stroke ${MAGENTA} 40`]);
  });

  it('shows erase strokes in violet', () => {
    const { ctx, calls } = recorder();
    paintMarks(
      ctx,
      100,
      100,
      MARK_KIND.erase,
      { strokes: [stroke('#fff', 0.04, [0, 0], [1, 1])], box: null },
      LOOK.show,
    );
    expect(calls.at(-1)).toBe(`stroke ${VIOLET} 4`);
  });

  it('keeps the pen colours of mark', () => {
    const { ctx, calls } = recorder();
    paintMarks(
      ctx,
      1000,
      500,
      MARK_KIND.mark,
      { strokes: [stroke('#ffd60a', PEN_WIDTH, [0, 0], [1, 1])], box: null },
      LOOK.export,
    );
    expect(calls.at(-1)).toBe('stroke #ffd60a 6');
  });

  it('draws a lone press as a dot', () => {
    const { ctx, calls } = recorder();
    paintMarks(
      ctx,
      100,
      100,
      MARK_KIND.mark,
      { strokes: [stroke('#fff', 0.01, [0.5, 0.5])], box: null },
      LOOK.export,
    );
    expect(calls).toContain('line 50.1 50');
  });

  it('writes a note as a thin magenta rectangle outline, whichever corner it was dragged from', () => {
    const { ctx, calls } = recorder();
    const marks: IMarks = { strokes: [], box: { x0: 0.6, y0: 0.8, x1: 0.2, y1: 0.4 } };
    paintMarks(ctx, 1000, 500, MARK_KIND.note, marks, LOOK.export);
    expect(calls).toEqual(['clear', `rect 200 200 400 200 ${MAGENTA} ${NOTE_WIDTH * 1000}`]);
  });

  it('draws nothing for a note without a frame', () => {
    const { ctx, calls } = recorder();
    paintMarks(ctx, 100, 100, MARK_KIND.note, { strokes: [], box: null }, LOOK.export);
    expect(calls).toEqual(['clear']);
  });
});

describe('overlaySize', () => {
  it('keeps the picture shape and caps the long side', () => {
    expect(overlaySize(4000, 2000)).toEqual({ w: 1024, h: 512 });
    expect(overlaySize(1000, 3000)).toEqual({ w: 341, h: 1024 });
  });
  it('never makes it larger than the picture', () => {
    expect(overlaySize(640, 480)).toEqual({ w: 640, h: 480 });
  });
});

describe('boxCounts', () => {
  it('ignores a stray press', () => {
    expect(boxCounts(null)).toBe(false);
    expect(boxCounts({ x0: 0.5, y0: 0.5, x1: 0.505, y1: 0.7 })).toBe(false);
    expect(boxCounts({ x0: 0.1, y0: 0.1, x1: 0.4, y1: 0.3 })).toBe(true);
  });
});
