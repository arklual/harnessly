// Marks the person puts on a picture themselves: a pen line, a frame around a place, an arrow, words, and
// the part of the picture to keep (a crop). Unlike the change tools (tool-rules.ts), nothing goes to a model: the marks
// are drawn into the picture here, and the result is a new PNG that goes into the field as an attachment.
// Points are fractions of the whole picture, widths and the size of the words fractions of its width, so the same
// marks are shown on the screen at any size and drawn into the picture's own pixels.
import {
  MARK_COLORS,
  clamp01,
  normalizedBox,
  type IBox,
  type IPoint,
} from '@/next/features/image-edit/marks';

export const MARKUP_TOOL = {
  pen: 'pen',
  frame: 'frame',
  arrow: 'arrow',
  text: 'text',
  crop: 'crop',
} as const;
export type TMarkupTool = (typeof MARKUP_TOOL)[keyof typeof MARKUP_TOOL];

export const MARKUP_TOOLS = [
  { id: MARKUP_TOOL.frame, icon: 'square' },
  { id: MARKUP_TOOL.arrow, icon: 'arrow-up-right' },
  { id: MARKUP_TOOL.text, icon: 'type' },
  { id: MARKUP_TOOL.pen, icon: 'pencil' },
  { id: MARKUP_TOOL.crop, icon: 'crop' },
] as const;

/** The line of the pen, the frame and the arrow. */
export const LINE_WIDTH = 0.007;
/** The height of the words. */
export const TEXT_SIZE = 0.045;
/** The arrow's head: its length as a multiple of the line, and its half-angle. */
export const HEAD_LENGTH = 4.5;
export const HEAD_ANGLE = Math.PI / 7;
/** A frame, an arrow or a crop shorter than this (of the picture's side) is a stray press. */
export const MIN_DRAG = 0.015;
/** The words are outlined in this, so they read on any picture. */
export const TEXT_OUTLINE = 'rgba(0, 0, 0, 0.6)';
/** What is cut off is shown darkened while the crop is being chosen. */
export const CROP_SHADE = 'rgba(0, 0, 0, 0.55)';
export const CROP_EDGE = '#ffffff';

export type TMarkupItem =
  | { kind: 'pen'; color: string; points: readonly IPoint[] }
  | { kind: 'frame'; color: string; box: IBox }
  | { kind: 'arrow'; color: string; from: IPoint; to: IPoint }
  | { kind: 'text'; color: string; at: IPoint; text: string };

export interface IMarkup {
  items: readonly TMarkupItem[];
  /** The part to keep, null: the whole picture. */
  crop: IBox | null;
}
export const NO_MARKUP: IMarkup = { items: [], crop: null };
export const DEFAULT_MARKUP_COLOR: string = MARK_COLORS[0];

/** The marks as they are and the states before them, for «Отменить». */
export interface IMarkupHistory {
  now: IMarkup;
  before: readonly IMarkup[];
}
export const freshHistory = (): IMarkupHistory => ({ now: NO_MARKUP, before: [] });

export const pushMarkup = (history: IMarkupHistory, next: IMarkup): IMarkupHistory => ({
  now: next,
  before: [...history.before, history.now],
});

export const undoMarkup = (history: IMarkupHistory): IMarkupHistory => {
  if (history.before.length === 0) return history;
  return { now: history.before[history.before.length - 1], before: history.before.slice(0, -1) };
};

export const addItem = (markup: IMarkup, item: TMarkupItem): IMarkup => ({
  ...markup,
  items: [...markup.items, item],
});

export const hasMarkup = (markup: IMarkup): boolean => markup.items.length > 0 || markup.crop !== null;

const dragCounts = (from: IPoint, to: IPoint): boolean =>
  Math.abs(to.x - from.x) >= MIN_DRAG || Math.abs(to.y - from.y) >= MIN_DRAG;

/** A drag of the frame, the arrow or the crop as what it leaves; null for a stray press. */
export const itemOfDrag = (
  tool: TMarkupTool,
  color: string,
  from: IPoint,
  to: IPoint,
): TMarkupItem | null => {
  if (!dragCounts(from, to)) return null;
  if (tool === MARKUP_TOOL.arrow) return { kind: 'arrow', color, from, to };
  if (tool === MARKUP_TOOL.frame) {
    const box = normalizedBox({ x0: from.x, y0: from.y, x1: to.x, y1: to.y });
    if (box.x1 - box.x0 < MIN_DRAG || box.y1 - box.y0 < MIN_DRAG) return null;
    return { kind: 'frame', color, box };
  }
  return null;
};

/** The crop a drag chooses; null for a drag too small to be one. */
export const cropOfDrag = (from: IPoint, to: IPoint): IBox | null => {
  const box = normalizedBox({
    x0: clamp01(from.x),
    y0: clamp01(from.y),
    x1: clamp01(to.x),
    y1: clamp01(to.y),
  });
  if (box.x1 - box.x0 < MIN_DRAG || box.y1 - box.y0 < MIN_DRAG) return null;
  return box;
};

/** The two ends of the arrow's head, beside its point. */
export const arrowHead = (from: IPoint, to: IPoint, length: number): [IPoint, IPoint] => {
  const angle = Math.atan2(to.y - from.y, to.x - from.x);
  const side = (turn: number): IPoint => ({
    x: to.x - length * Math.cos(angle + turn),
    y: to.y - length * Math.sin(angle + turn),
  });
  return [side(HEAD_ANGLE), side(-HEAD_ANGLE)];
};

/** The crop in the picture's pixels: whole numbers, inside the picture, at least one pixel. */
export const cropPixels = (
  crop: IBox | null,
  w: number,
  h: number,
): { x: number; y: number; w: number; h: number } => {
  if (crop === null) return { x: 0, y: 0, w, h };
  const box = normalizedBox(crop);
  const x = Math.min(w - 1, Math.max(0, Math.round(box.x0 * w)));
  const y = Math.min(h - 1, Math.max(0, Math.round(box.y0 * h)));
  const right = Math.min(w, Math.max(x + 1, Math.round(box.x1 * w)));
  const bottom = Math.min(h, Math.max(y + 1, Math.round(box.y1 * h)));
  return { x, y, w: right - x, h: bottom - y };
};

/** The name of the marked picture: the same when it takes the place of an attachment, else «<name>-marked.png». */
export const markedName = (name: string, replacing: boolean): string => {
  const dot = name.lastIndexOf('.');
  const base = dot > 0 ? name.slice(0, dot) : name;
  const clean = base.trim() === '' ? 'image' : base.trim();
  return replacing ? `${clean}.png` : `${clean}-marked.png`;
};

export const MARKUP_LOOK = { show: 'show', export: 'export' } as const;
export type TMarkupLook = (typeof MARKUP_LOOK)[keyof typeof MARKUP_LOOK];

/** The part of a 2D context the marks need (a test hands in a recorder). */
export type TMarkupContext = Pick<
  CanvasRenderingContext2D,
  | 'beginPath'
  | 'moveTo'
  | 'lineTo'
  | 'closePath'
  | 'stroke'
  | 'fill'
  | 'strokeRect'
  | 'fillRect'
  | 'clearRect'
  | 'strokeText'
  | 'fillText'
  | 'save'
  | 'restore'
> & {
  lineWidth: number;
  strokeStyle: string | CanvasGradient | CanvasPattern;
  fillStyle: string | CanvasGradient | CanvasPattern;
  lineCap: CanvasLineCap;
  lineJoin: CanvasLineJoin;
  font: string;
  textBaseline: CanvasTextBaseline;
};

/** Words not yet placed: shown where they will stand while they are typed. */
export interface IPendingText {
  at: IPoint;
  text: string;
  color: string;
}

const paintItem = (ctx: TMarkupContext, w: number, h: number, item: TMarkupItem): void => {
  const line = Math.max(2, LINE_WIDTH * w);
  ctx.lineWidth = line;
  ctx.strokeStyle = item.color;
  ctx.fillStyle = item.color;
  switch (item.kind) {
    case 'pen': {
      if (item.points.length === 0) return;
      const [first, ...rest] = item.points;
      ctx.beginPath();
      ctx.moveTo(first.x * w, first.y * h);
      if (rest.length === 0) ctx.lineTo(first.x * w + 0.1, first.y * h);
      for (const point of rest) ctx.lineTo(point.x * w, point.y * h);
      ctx.stroke();
      return;
    }
    case 'frame': {
      const box = normalizedBox(item.box);
      ctx.strokeRect(box.x0 * w, box.y0 * h, (box.x1 - box.x0) * w, (box.y1 - box.y0) * h);
      return;
    }
    case 'arrow': {
      const from = { x: item.from.x * w, y: item.from.y * h };
      const to = { x: item.to.x * w, y: item.to.y * h };
      const [left, right] = arrowHead(from, to, line * HEAD_LENGTH);
      ctx.beginPath();
      ctx.moveTo(from.x, from.y);
      ctx.lineTo(to.x, to.y);
      ctx.stroke();
      ctx.beginPath();
      ctx.moveTo(to.x, to.y);
      ctx.lineTo(left.x, left.y);
      ctx.lineTo(right.x, right.y);
      ctx.closePath();
      ctx.fill();
      ctx.stroke();
      return;
    }
    case 'text': {
      const size = Math.max(10, TEXT_SIZE * w);
      ctx.font = `600 ${size}px system-ui, -apple-system, 'Segoe UI', sans-serif`;
      ctx.textBaseline = 'middle';
      ctx.lineWidth = Math.max(2, size / 6);
      ctx.strokeStyle = TEXT_OUTLINE;
      ctx.strokeText(item.text, item.at.x * w, item.at.y * h);
      ctx.fillStyle = item.color;
      ctx.fillText(item.text, item.at.x * w, item.at.y * h);
      return;
    }
  }
};

/** The crop being chosen: the rest is darkened, the kept part has a thin white edge. */
const paintCrop = (ctx: TMarkupContext, w: number, h: number, crop: IBox): void => {
  const box = normalizedBox(crop);
  const x0 = box.x0 * w;
  const y0 = box.y0 * h;
  const x1 = box.x1 * w;
  const y1 = box.y1 * h;
  ctx.fillStyle = CROP_SHADE;
  ctx.fillRect(0, 0, w, y0);
  ctx.fillRect(0, y1, w, h - y1);
  ctx.fillRect(0, y0, x0, y1 - y0);
  ctx.fillRect(x1, y0, w - x1, y1 - y0);
  ctx.lineWidth = Math.max(1, w / 400);
  ctx.strokeStyle = CROP_EDGE;
  ctx.strokeRect(x0, y0, x1 - x0, y1 - y0);
};

/**
 * Draws the marks on a canvas of `w`×`h` (the whole picture). On the screen the canvas is clear under the marks and the
 * crop is shown; into the picture the crop is not drawn, it is where the picture is cut.
 */
export const paintMarkup = (
  ctx: TMarkupContext,
  w: number,
  h: number,
  markup: IMarkup,
  look: TMarkupLook,
  pending: IPendingText | null = null,
): void => {
  if (look === MARKUP_LOOK.show) ctx.clearRect(0, 0, w, h);
  ctx.save();
  ctx.lineCap = 'round';
  ctx.lineJoin = 'round';
  for (const item of markup.items) paintItem(ctx, w, h, item);
  if (pending !== null && pending.text.trim() !== '')
    paintItem(ctx, w, h, { kind: 'text', color: pending.color, at: pending.at, text: pending.text });
  ctx.restore();
  if (look === MARKUP_LOOK.show && markup.crop !== null) paintCrop(ctx, w, h, markup.crop);
};

/**
 * Reads the picture's bytes (a same-origin address of the machine) and decodes them from a local address, so the canvas
 * it is drawn on can be read back; an <img> decodes what createImageBitmap does not (an SVG).
 */
const loadPicture = async (src: string): Promise<{ image: HTMLImageElement; done: () => void }> => {
  const res = await fetch(src, { credentials: 'same-origin' });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const local = URL.createObjectURL(await res.blob());
  const image = new Image();
  image.src = local;
  try {
    await image.decode();
  } catch (failure) {
    URL.revokeObjectURL(local);
    throw failure;
  }
  return { image, done: () => URL.revokeObjectURL(local) };
};

/** The picture with its marks drawn in and cut to the crop, as a PNG file. */
export const renderMarkup = async (src: string, markup: IMarkup, name: string): Promise<File> => {
  const { image, done } = await loadPicture(src);
  const w = image.naturalWidth;
  const h = image.naturalHeight;
  const part = cropPixels(markup.crop, w, h);
  const canvas = document.createElement('canvas');
  canvas.width = part.w;
  canvas.height = part.h;
  const ctx = canvas.getContext('2d');
  if (ctx === null) throw new Error('no 2d canvas');
  ctx.translate(-part.x, -part.y);
  ctx.drawImage(image, 0, 0, w, h);
  paintMarkup(ctx, w, h, markup, MARKUP_LOOK.export);
  done();
  const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, 'image/png'));
  if (blob === null) throw new Error('no png');
  return new File([blob], name, { type: 'image/png' });
};
