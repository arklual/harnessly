// What the person draws on a picture, kept as data (points as fractions of the picture, widths as fractions of its
// width) so the same marks are shown on the screen and written out as the overlay PNG the server reads. The overlay is
// stretched to the picture, so it can be drawn at any size of the picture's shape.

export const MARK_COLORS = ['#ff3b5c', '#ffd60a', '#ffffff'] as const;
export type TMarkColor = (typeof MARK_COLORS)[number];

/** The mark the server's brief calls «bright magenta»: `erase` and `note` are written in it, opaque. */
export const MAGENTA = '#ff00ff';
/** How `erase` and `note` look to the person (the canvas is translucent over the picture, see image-edit.css). */
export const VIOLET = '#7b61ff';

/** The pen of `mark`, and the outline of `note` (a thin rectangle, about half a percent of the width). */
export const PEN_WIDTH = 0.006;
export const NOTE_WIDTH = 0.005;

/** The widths of the brush of `erase`, as a fraction of the picture's width. */
export const BRUSH_WIDTH = { thin: 0.02, medium: 0.04, wide: 0.07 } as const;
export type TBrushId = keyof typeof BRUSH_WIDTH;
export const BRUSH_IDS = Object.keys(BRUSH_WIDTH) as TBrushId[];
export const DEFAULT_BRUSH: TBrushId = 'medium';

/** A frame smaller than this (of the picture's side) is a stray press, not a frame. */
export const MIN_BOX = 0.02;
/** The long side of the exported overlay at most: the picture's own shape, never larger than its pixels. */
export const OVERLAY_MAX_SIDE = 1024;

export interface IPoint {
  x: number;
  y: number;
}
export interface IStroke {
  /** Only `mark` has colours; `erase` is written in magenta whatever this says. */
  color: string;
  /** A fraction of the picture's width. */
  width: number;
  points: readonly IPoint[];
}
export interface IBox {
  x0: number;
  y0: number;
  x1: number;
  y1: number;
}
export interface IMarks {
  strokes: readonly IStroke[];
  box: IBox | null;
}
export const NO_MARKS: IMarks = { strokes: [], box: null };

export const MARK_KIND = { mark: 'mark', erase: 'erase', note: 'note' } as const;
export type TMarkKind = (typeof MARK_KIND)[keyof typeof MARK_KIND];

export const LOOK = { show: 'show', export: 'export' } as const;
export type TLook = (typeof LOOK)[keyof typeof LOOK];

/** The part of a 2D context the marks need (a test hands in a recorder). */
export type TPaintContext = Pick<
  CanvasRenderingContext2D,
  'beginPath' | 'moveTo' | 'lineTo' | 'stroke' | 'strokeRect' | 'clearRect'
> & {
  lineWidth: number;
  strokeStyle: string | CanvasGradient | CanvasPattern;
  lineCap: CanvasLineCap;
  lineJoin: CanvasLineJoin;
};

export const clamp01 = (value: number): number => Math.min(1, Math.max(0, value));

export const normalizedBox = (box: IBox): IBox => ({
  x0: Math.min(box.x0, box.x1),
  y0: Math.min(box.y0, box.y1),
  x1: Math.max(box.x0, box.x1),
  y1: Math.max(box.y0, box.y1),
});

/** A frame big enough to mean something. */
export const boxCounts = (box: IBox | null): box is IBox =>
  box !== null && Math.abs(box.x1 - box.x0) >= MIN_BOX && Math.abs(box.y1 - box.y0) >= MIN_BOX;

const colorOf = (kind: TMarkKind, look: TLook, stroke: IStroke | null): string => {
  if (kind === MARK_KIND.mark) return stroke?.color ?? MARK_COLORS[0];
  return look === LOOK.export ? MAGENTA : VIOLET;
};

/** Draws the marks of one tool on a canvas of `w`×`h`, transparent elsewhere. */
export const paintMarks = (
  ctx: TPaintContext,
  w: number,
  h: number,
  kind: TMarkKind,
  marks: IMarks,
  look: TLook,
): void => {
  ctx.clearRect(0, 0, w, h);
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  if (kind === MARK_KIND.note) {
    if (marks.box === null) return;
    const box = normalizedBox(marks.box);
    // On the screen the frame is a finer line than what the server reads.
    ctx.lineWidth = look === LOOK.export ? Math.max(1, NOTE_WIDTH * w) : Math.max(2, (NOTE_WIDTH * w) / 2);
    ctx.strokeStyle = colorOf(kind, look, null);
    ctx.strokeRect(box.x0 * w, box.y0 * h, (box.x1 - box.x0) * w, (box.y1 - box.y0) * h);
    return;
  }
  for (const stroke of marks.strokes) {
    if (stroke.points.length === 0) continue;
    ctx.lineWidth = stroke.width * w;
    ctx.strokeStyle = colorOf(kind, look, stroke);
    ctx.beginPath();
    const [first, ...rest] = stroke.points;
    ctx.moveTo(first.x * w, first.y * h);
    // A press without a move is a dot: a line to (almost) the same place, round-capped.
    if (rest.length === 0) ctx.lineTo(first.x * w + 0.1, first.y * h);
    for (const point of rest) ctx.lineTo(point.x * w, point.y * h);
    ctx.stroke();
  }
};

/** The overlay's size: the picture's shape, its long side at most OVERLAY_MAX_SIDE and never more than its pixels. */
export const overlaySize = (naturalW: number, naturalH: number): { w: number; h: number } => {
  const long = Math.max(naturalW, naturalH);
  const scale = long > OVERLAY_MAX_SIDE ? OVERLAY_MAX_SIDE / long : 1;
  return { w: Math.max(1, Math.round(naturalW * scale)), h: Math.max(1, Math.round(naturalH * scale)) };
};

/** The overlay as the server wants it: a base64 PNG (no `data:` prefix), drawn on an offscreen canvas. */
export const exportOverlay = (kind: TMarkKind, marks: IMarks, naturalW: number, naturalH: number): string => {
  const { w, h } = overlaySize(naturalW, naturalH);
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext('2d');
  if (ctx === null) throw new Error('no 2d canvas');
  paintMarks(ctx, w, h, kind, marks, LOOK.export);
  const url = canvas.toDataURL('image/png');
  return url.slice(url.indexOf(',') + 1);
};
