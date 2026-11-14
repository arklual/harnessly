import { describe, expect, it } from 'vitest';
import {
  CROP_SHADE,
  MARKUP_LOOK,
  MARKUP_TOOL,
  NO_MARKUP,
  addItem,
  arrowHead,
  cropOfDrag,
  cropPixels,
  freshHistory,
  hasMarkup,
  itemOfDrag,
  markedName,
  paintMarkup,
  pushMarkup,
  undoMarkup,
  type IMarkup,
  type TMarkupContext,
} from '@/next/features/image-edit/markup';

const recorder = () => {
  const calls: string[] = [];
  const ctx = {
    lineWidth: 0,
    strokeStyle: '' as string,
    fillStyle: '' as string,
    lineCap: 'butt' as CanvasLineCap,
    lineJoin: 'miter' as CanvasLineJoin,
    font: '',
    textBaseline: 'alphabetic' as CanvasTextBaseline,
    clearRect: () => calls.push('clear'),
    beginPath: () => calls.push('begin'),
    closePath: () => calls.push('close'),
    moveTo: (x: number, y: number) => calls.push(`move ${Math.round(x)} ${Math.round(y)}`),
    lineTo: (x: number, y: number) => calls.push(`line ${Math.round(x)} ${Math.round(y)}`),
    stroke: () => calls.push(`stroke ${ctx.strokeStyle}`),
    fill: () => calls.push(`fill ${ctx.fillStyle}`),
    strokeRect: (x: number, y: number, w: number, h: number) =>
      calls.push(`rect ${[x, y, w, h].map(Math.round).join(' ')} ${ctx.strokeStyle}`),
    fillRect: () => calls.push(`shade ${ctx.fillStyle}`),
    strokeText: (text: string) => calls.push(`outline ${text}`),
    fillText: (text: string, x: number, y: number) =>
      calls.push(`text ${text} ${Math.round(x)} ${Math.round(y)} ${ctx.fillStyle}`),
    save: () => calls.push('save'),
    restore: () => calls.push('restore'),
  };
  return { ctx: ctx as unknown as TMarkupContext, calls };
};

describe('drags', () => {
  it('a frame is normalised, a stray press leaves nothing', () => {
    expect(itemOfDrag(MARKUP_TOOL.frame, '#fff', { x: 0.6, y: 0.5 }, { x: 0.2, y: 0.1 })).toEqual({
      kind: 'frame',
      color: '#fff',
      box: { x0: 0.2, y0: 0.1, x1: 0.6, y1: 0.5 },
    });
    expect(itemOfDrag(MARKUP_TOOL.frame, '#fff', { x: 0.5, y: 0.5 }, { x: 0.505, y: 0.505 })).toBeNull();
    // A frame flat in one direction is no frame either.
    expect(itemOfDrag(MARKUP_TOOL.frame, '#fff', { x: 0.1, y: 0.5 }, { x: 0.6, y: 0.5 })).toBeNull();
  });

  it('an arrow keeps its direction', () => {
    expect(itemOfDrag(MARKUP_TOOL.arrow, '#fff', { x: 0.8, y: 0.2 }, { x: 0.1, y: 0.9 })).toEqual({
      kind: 'arrow',
      color: '#fff',
      from: { x: 0.8, y: 0.2 },
      to: { x: 0.1, y: 0.9 },
    });
  });

  it('a crop is clamped to the picture', () => {
    expect(cropOfDrag({ x: -0.2, y: 0.9 }, { x: 0.5, y: 1.4 })).toEqual({ x0: 0, y0: 0.9, x1: 0.5, y1: 1 });
    expect(cropOfDrag({ x: 0.3, y: 0.3 }, { x: 0.31, y: 0.9 })).toBeNull();
  });
});

describe('arrowHead', () => {
  it('both ends stand behind the point, one on each side', () => {
    const [a, b] = arrowHead({ x: 0, y: 0 }, { x: 100, y: 0 }, 10);
    expect(a.x).toBeLessThan(100);
    expect(b.x).toBeLessThan(100);
    expect(Math.sign(a.y)).toBe(-Math.sign(b.y));
    expect(Math.hypot(100 - a.x, a.y)).toBeCloseTo(10);
  });
});

describe('cropPixels', () => {
  it('the whole picture without a crop', () => {
    expect(cropPixels(null, 640, 480)).toEqual({ x: 0, y: 0, w: 640, h: 480 });
  });
  it('whole pixels inside the picture, at least one', () => {
    expect(cropPixels({ x0: 0.5, y0: 0.25, x1: 0.1, y1: 0.75 }, 1000, 400)).toEqual({
      x: 100,
      y: 100,
      w: 400,
      h: 200,
    });
    expect(cropPixels({ x0: 1, y0: 1, x1: 1, y1: 1 }, 10, 10)).toEqual({ x: 9, y: 9, w: 1, h: 1 });
  });
});

describe('markedName', () => {
  it('a new attachment says it is marked, a replaced one keeps its name', () => {
    expect(markedName('image.png', false)).toBe('image-marked.png');
    expect(markedName('photo.final.jpg', true)).toBe('photo.final.png');
    expect(markedName('', false)).toBe('image-marked.png');
    expect(markedName('notes', true)).toBe('notes.png');
  });
});

describe('history', () => {
  it('undo walks back through every change, the crop too', () => {
    const frame = { kind: 'frame' as const, color: '#fff', box: { x0: 0, y0: 0, x1: 0.5, y1: 0.5 } };
    let history = freshHistory();
    expect(hasMarkup(history.now)).toBe(false);
    history = pushMarkup(history, addItem(history.now, frame));
    history = pushMarkup(history, { ...history.now, crop: { x0: 0, y0: 0, x1: 0.5, y1: 0.5 } });
    expect(hasMarkup(history.now)).toBe(true);
    history = undoMarkup(history);
    expect(history.now.crop).toBeNull();
    expect(history.now.items).toHaveLength(1);
    history = undoMarkup(history);
    expect(history.now).toBe(NO_MARKUP);
    expect(undoMarkup(history)).toBe(history);
  });
});

describe('paintMarkup', () => {
  const markup: IMarkup = {
    items: [
      { kind: 'arrow', color: '#ff3b5c', from: { x: 0, y: 0.5 }, to: { x: 0.5, y: 0.5 } },
      { kind: 'text', color: '#ffd60a', at: { x: 0.1, y: 0.2 }, text: 'Здесь' },
    ],
    crop: { x0: 0.1, y0: 0.1, x1: 0.9, y1: 0.9 },
  };

  it('on the screen: clears, draws the marks, shades what the crop cuts off', () => {
    const { ctx, calls } = recorder();
    paintMarkup(ctx, 200, 100, markup, MARKUP_LOOK.show, {
      at: { x: 0.5, y: 0.9 },
      text: 'ещё',
      color: '#ffffff',
    });
    expect(calls[0]).toBe('clear');
    expect(calls).toContain('move 0 50');
    expect(calls).toContain('line 100 50');
    expect(calls).toContain('fill #ff3b5c');
    expect(calls).toContain('text Здесь 20 20 #ffd60a');
    expect(calls).toContain('text ещё 100 90 #ffffff');
    expect(calls.filter((call) => call === `shade ${CROP_SHADE}`)).toHaveLength(4);
  });

  it('into the picture: no clearing, no shade — the crop is where it is cut', () => {
    const { ctx, calls } = recorder();
    paintMarkup(ctx, 200, 100, markup, MARKUP_LOOK.export);
    expect(calls).not.toContain('clear');
    expect(calls.some((call) => call.startsWith('shade'))).toBe(false);
    expect(calls).toContain('outline Здесь');
  });
});
