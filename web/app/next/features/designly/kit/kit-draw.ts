// The kit canvas drawn on a <canvas>: hundreds of cells cost no elements. Only the cells the viewport sees are drawn
// (a grid knows its visible range at once). Far away a cell is its piece of the group's atlas (one picture for the
// whole group), closer its own picture, loaded as it comes into view, a few at a time; near cells get a live page
// over the canvas (kit-live.tsx), which this leaves room for.
import {
  cellRect,
  ITEM,
  visibleCells,
  type IBlock,
  type IWorld,
  type IWorldItem,
} from '@/next/features/designly/kit/kit-world';
import { lodOf, LOD, viewRect, type ICamera } from '@/next/features/designly/kit/kit-camera';
import type { IAtlas } from '@/next/features/designly/kit/kit-api';

/** The canvas's colours, read from the area's custom properties (styles/kit.css), so both themes hold. */
export interface IPalette {
  canvas: string;
  frame: string;
  frameLine: string;
  title: string;
  caption: string;
  sub: string;
  card: string;
  cardLine: string;
  select: string;
  hover: string;
  font: string;
}

export const readPalette = (el: HTMLElement): IPalette => {
  const cs = getComputedStyle(el);
  const v = (name: string) => cs.getPropertyValue(name).trim();
  return {
    canvas: v('--kit-canvas'),
    frame: v('--kit-frame'),
    frameLine: v('--kit-frame-line'),
    title: v('--kit-title'),
    caption: v('--kit-caption'),
    sub: v('--kit-sub'),
    card: v('--kit-card'),
    cardLine: v('--kit-card-line'),
    select: v('--kit-select'),
    hover: v('--kit-hover'),
    font: cs.fontFamily,
  };
};

const LOADING = 0;
const READY = 1;
const FAILED = 2;

interface IPic {
  img: HTMLImageElement;
  state: typeof LOADING | typeof READY | typeof FAILED;
  used: number;
}

/**
 * Pictures by address, loaded a few at a time, the ones asked for last first (what the viewport sees now). A failed
 * one is not asked again; the least recently drawn are let go past the limit.
 */
export class PictureCache {
  private pics = new Map<string, IPic>();
  private queue: string[] = [];
  private running = 0;
  private tick = 0;

  constructor(
    private readonly onReady: () => void,
    private readonly parallel = 6,
    private readonly limit = 600,
  ) {}

  /** The picture when it is ready; otherwise it is asked for and null comes back. */
  get(url: string): HTMLImageElement | null {
    const pic = this.pics.get(url);
    this.tick += 1;
    if (pic) {
      pic.used = this.tick;
      return pic.state === READY ? pic.img : null;
    }
    if (!this.queue.includes(url)) this.queue.push(url);
    this.pump();
    return null;
  }

  /** Drops what was asked for but not started: the viewport moved on. */
  forgetQueued() {
    this.queue = [];
  }

  private pump() {
    while (this.running < this.parallel && this.queue.length > 0) {
      const url = this.queue.pop() as string;
      if (this.pics.has(url)) continue;
      const img = new Image();
      img.decoding = 'async';
      const pic: IPic = { img, state: LOADING, used: this.tick };
      this.pics.set(url, pic);
      this.running += 1;
      const done = (state: IPic['state']) => {
        pic.state = state;
        this.running -= 1;
        this.trim();
        this.pump();
        if (state === READY) this.onReady();
      };
      img.onload = () => done(READY);
      img.onerror = () => done(FAILED);
      img.src = url;
    }
  }

  private trim() {
    if (this.pics.size <= this.limit) return;
    const old = [...this.pics.entries()]
      .filter(([, p]) => p.state !== LOADING)
      .sort((a, b) => a[1].used - b[1].used)
      .slice(0, this.pics.size - this.limit);
    for (const [url] of old) this.pics.delete(url);
  }
}

export interface IDrawInput {
  world: IWorld;
  camera: ICamera;
  area: { w: number; h: number };
  dpr: number;
  palette: IPalette;
  pictures: PictureCache;
  /** The address of a theme's file, of an atlas by key. */
  fileUrl: (file: string) => string;
  atlases: ReadonlyMap<string, { atlas: IAtlas; url: string }>;
  selected: string | null;
  hovered: string | null;
  /** Cells a live page covers: their picture is not drawn under it. */
  live: ReadonlySet<string>;
}

/** What the drawing found on its way: the cells shown near enough for a live page, in screen pixels. */
export interface INearCell {
  node: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

const rounded = (ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) => {
  ctx.beginPath();
  ctx.roundRect(x, y, w, h, Math.min(r, w / 2, h / 2));
};

const ellipsize = (ctx: CanvasRenderingContext2D, text: string, max: number): string => {
  if (ctx.measureText(text).width <= max) return text;
  let lo = 0;
  let hi = text.length;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (ctx.measureText(`${text.slice(0, mid)}…`).width <= max) lo = mid;
    else hi = mid - 1;
  }
  return `${text.slice(0, lo)}…`;
};

const atlasCell = (a: IAtlas, cell: number) => ({
  sx: (cell % a.cols) * a.cellW,
  sy: Math.floor(cell / a.cols) * a.cellH,
});

/** Draws a picture fitted into a box, keeping its aspect, centred. */
const drawFitted = (
  ctx: CanvasRenderingContext2D,
  img: CanvasImageSource,
  sx: number,
  sy: number,
  sw: number,
  sh: number,
  x: number,
  y: number,
  w: number,
  h: number,
  cover: boolean,
) => {
  const k = cover ? Math.max(w / sw, h / sh) : Math.min(w / sw, h / sh);
  const dw = sw * k;
  const dh = sh * k;
  if (cover) {
    ctx.save();
    ctx.beginPath();
    ctx.rect(x, y, w, h);
    ctx.clip();
    ctx.drawImage(img, sx, sy, sw, sh, x + (w - dw) / 2, y + (h - dh) / 2, dw, dh);
    ctx.restore();
    return;
  }
  ctx.drawImage(img, sx, sy, sw, sh, x + (w - dw) / 2, y + (h - dh) / 2, dw, dh);
};

/** A component's card as a grid of the assets it takes (Icon: real icons), false until their atlas is loaded. */
const drawSamples = (
  ctx: CanvasRenderingContext2D,
  d: IDrawInput,
  samples: { atlas: string; cells: number[] },
  x: number,
  y: number,
  w: number,
  h: number,
): boolean => {
  const entry = d.atlases.get(samples.atlas);
  const sheet = entry ? d.pictures.get(entry.url) : null;
  if (!entry || !sheet) return false;
  const cols = 6;
  const rows = Math.ceil(samples.cells.length / cols);
  const pad = Math.min(w, h) * 0.08;
  const cw = (w - pad * 2) / cols;
  const ch = (h - pad * 2) / rows;
  const s = Math.min(cw, ch) * 0.7;
  samples.cells.forEach((cell, i) => {
    const { sx, sy } = atlasCell(entry.atlas, cell);
    const cx = x + pad + (i % cols) * cw + (cw - s) / 2;
    const cy = y + pad + Math.floor(i / cols) * ch + (ch - s) / 2;
    drawFitted(ctx, sheet, sx, sy, entry.atlas.cellW, entry.atlas.cellH, cx, cy, s, s, false);
  });
  return true;
};

/** A whole picture fitted into a box: scaled as one, never cut by a source rectangle (an SVG without its own size
 * is drawn wrong when a part of it is asked for). */
const drawWhole = (
  ctx: CanvasRenderingContext2D,
  img: HTMLImageElement,
  x: number,
  y: number,
  w: number,
  h: number,
) => {
  const iw = img.naturalWidth || 1;
  const ih = img.naturalHeight || 1;
  const k = Math.min(w / iw, h / ih);
  ctx.drawImage(img, x + (w - iw * k) / 2, y + (h - ih * k) / 2, iw * k, ih * k);
};

const PICTURE_PAD: Partial<Record<IWorldItem['kind'], number>> = {
  [ITEM.icon]: 0.18,
  [ITEM.image]: 0.08,
  [ITEM.logo]: 0.12,
};

/** Draws the world as the camera shows it; answers the cells near enough for a live page. */
export const drawWorld = (ctx: CanvasRenderingContext2D, d: IDrawInput): INearCell[] => {
  const { camera: cam, palette: pal, area } = d;
  const near: INearCell[] = [];
  ctx.setTransform(d.dpr, 0, 0, d.dpr, 0, 0);
  ctx.fillStyle = pal.canvas;
  ctx.fillRect(0, 0, area.w, area.h);
  const view = viewRect(cam, area);
  const margin = 40 / cam.s;
  const seen = { x: view.x - margin, y: view.y - margin, w: view.w + margin * 2, h: view.h + margin * 2 };
  const sx = (x: number) => x * cam.s + cam.x;
  const sy = (y: number) => y * cam.s + cam.y;

  // Frames: a section's plate, its title above it at a size that reads at any zoom.
  for (const f of d.world.frames) {
    if (f.x > seen.x + seen.w || f.x + f.w < seen.x || f.y > seen.y + seen.h || f.y + f.h < seen.y) continue;
    const x = sx(f.x);
    const y = sy(f.y);
    const w = f.w * cam.s;
    const h = f.h * cam.s;
    if (f.depth === 0) {
      rounded(ctx, x, y, w, h, 16 * Math.min(1, cam.s * 4));
      ctx.fillStyle = f.node === d.selected ? pal.hover : pal.frame;
      ctx.fill();
      ctx.lineWidth = f.node === d.selected ? 2 : 1;
      ctx.strokeStyle = f.node === d.selected ? pal.select : pal.frameLine;
      ctx.stroke();
      const px = Math.max(13, Math.min(28, 40 * cam.s));
      ctx.font = `600 ${px}px ${pal.font}`;
      ctx.fillStyle = pal.title;
      ctx.textBaseline = 'bottom';
      const title = f.count ? `${f.title}  ${f.count}` : f.title;
      ctx.fillText(ellipsize(ctx, title, Math.max(40, w)), x + 2, y - 8);
    } else if (cam.s * 26 >= 9) {
      const px = Math.min(22, 26 * cam.s);
      ctx.font = `500 ${px}px ${pal.font}`;
      ctx.fillStyle = f.node === d.selected ? pal.select : pal.caption;
      ctx.textBaseline = 'top';
      const title = f.count ? `${f.title}  ${f.count}` : f.title;
      ctx.fillText(ellipsize(ctx, title, Math.max(40, w)), x, y + 4 * cam.s);
    }
  }

  for (const b of d.world.blocks) drawBlock(ctx, d, b, seen, near);
  return near;
};

const drawBlock = (
  ctx: CanvasRenderingContext2D,
  d: IDrawInput,
  b: IBlock,
  seen: { x: number; y: number; w: number; h: number },
  near: INearCell[],
) => {
  const range = visibleCells(b, seen);
  if (!range) return;
  const { camera: cam, palette: pal } = d;
  const cw = b.cellW * cam.s;
  const ch = b.cellH * cam.s;
  const tiny = cw < 6;
  for (let i = range.from; i <= range.to; i++) {
    const col = i % b.cols;
    if (col < range.c0 || col > range.c1) continue;
    const it = b.items[i];
    if (!it) continue;
    const r = cellRect(b, i);
    const x = r.x * cam.s + cam.x;
    const y = r.y * cam.s + cam.y;
    if (tiny) {
      ctx.fillStyle = it.fill ?? pal.cardLine;
      ctx.fillRect(x, y, Math.max(1, cw), Math.max(1, ch));
      continue;
    }
    const selected = it.node === d.selected;
    const hovered = it.node === d.hovered;
    drawCell(ctx, d, it, x, y, cw, ch);
    if (selected || hovered) {
      rounded(ctx, x - 3, y - 3, cw + 6, ch + 6, 10 * Math.min(1, cam.s * 2));
      ctx.lineWidth = selected ? 2.5 : 1.5;
      ctx.strokeStyle = selected ? pal.select : pal.hover;
      ctx.stroke();
    }
    // Captions only where they read.
    const capH = b.captionH * cam.s;
    if (capH >= 11) {
      const px = Math.min(15, capH * 0.42);
      ctx.font = `500 ${px}px ${pal.font}`;
      ctx.fillStyle = selected ? pal.select : pal.title;
      ctx.textBaseline = 'top';
      ctx.fillText(ellipsize(ctx, it.label, cw), x, y + ch + capH * 0.16);
      if (it.sub && capH >= 26) {
        ctx.font = `400 ${px * 0.9}px ${pal.font}`;
        ctx.fillStyle = pal.sub;
        ctx.fillText(ellipsize(ctx, it.sub, cw), x, y + ch + capH * 0.16 + px * 1.3);
      }
    }
    if ((it.kind === ITEM.layout || it.kind === ITEM.component || it.kind === ITEM.stack) && cw >= 520)
      near.push({ node: it.node, x, y, w: cw, h: ch });
  }
};

const drawCell = (
  ctx: CanvasRenderingContext2D,
  d: IDrawInput,
  it: IWorldItem,
  x: number,
  y: number,
  w: number,
  h: number,
) => {
  const { palette: pal, camera: cam } = d;
  const radius = 8 * Math.min(1, cam.s * 2);
  if (it.kind === ITEM.color) {
    rounded(ctx, x, y, w, h, radius);
    ctx.fillStyle = it.fill ?? pal.card;
    ctx.fill();
    ctx.lineWidth = 1;
    ctx.strokeStyle = pal.cardLine;
    ctx.stroke();
    return;
  }
  // The stack: two plates behind the cover say it holds more.
  if (it.kind === ITEM.stack) {
    for (const k of [2, 1]) {
      const o = k * 10 * cam.s;
      rounded(ctx, x + o, y - o, w, h, radius);
      ctx.fillStyle = pal.card;
      ctx.fill();
      ctx.strokeStyle = pal.cardLine;
      ctx.lineWidth = 1;
      ctx.stroke();
    }
  }
  rounded(ctx, x, y, w, h, radius);
  ctx.fillStyle = pal.card;
  ctx.fill();
  ctx.lineWidth = 1;
  ctx.strokeStyle = pal.cardLine;
  ctx.stroke();
  if (it.kind === ITEM.type && it.font) {
    const px = Math.min(it.font.px * cam.s, h * 0.7);
    if (px >= 4) {
      ctx.font = `${it.font.weight} ${px}px ${it.font.family}`;
      ctx.fillStyle = pal.title;
      ctx.textBaseline = 'middle';
      ctx.fillText(
        ellipsize(ctx, 'Аа Бб Вв — съешь же ещё этих мягких булок', w - 24 * cam.s),
        x + 16 * cam.s,
        y + h / 2,
      );
    }
    return;
  }
  if (it.kind === ITEM.component && it.samples && drawSamples(ctx, d, it.samples, x, y, w, h)) return;
  // A component with a picture (its thumb, or its cell of the group's atlas) is drawn as that picture below.
  if (
    it.kind === ITEM.token ||
    (it.kind === ITEM.component && it.file === undefined && it.atlas === undefined)
  ) {
    const px = Math.min(18, Math.max(0, (it.kind === ITEM.component ? 28 : 20) * cam.s));
    if (px >= 6) {
      ctx.font = `600 ${px}px ${pal.font}`;
      ctx.fillStyle = pal.caption;
      ctx.textBaseline = 'middle';
      ctx.fillText(
        ellipsize(ctx, it.kind === ITEM.component ? `<${it.label}/>` : (it.sub ?? ''), w - 16),
        x + 12 * cam.s,
        y + h / 2,
      );
    }
    return;
  }
  if (d.live.has(it.node)) return;
  const pad = (PICTURE_PAD[it.kind] ?? 0) * Math.min(w, h);
  const bx = x + pad;
  const by = y + pad;
  const bw = w - pad * 2;
  const bh = h - pad * 2;
  const cover = it.kind === ITEM.layout || it.kind === ITEM.stack;
  const entry = it.atlas !== undefined ? d.atlases.get(it.atlas) : undefined;
  const lod = lodOf(w, entry ? entry.atlas.cellW : 0);
  // A picture the atlas does not hold (a vector logo) is its own file at every distance, or far away it is empty.
  const inAtlas = entry !== undefined && it.cell !== undefined;
  const own = it.file && (lod !== LOD.far || !inAtlas) ? d.pictures.get(d.fileUrl(it.file)) : null;
  if (it.bg) {
    ctx.fillStyle = it.bg;
    rounded(ctx, x, y, w, h, radius);
    ctx.fill();
  }
  if (own) {
    ctx.save();
    rounded(ctx, x, y, w, h, radius);
    ctx.clip();
    if (cover) drawFitted(ctx, own, 0, 0, own.naturalWidth, own.naturalHeight, bx, by, bw, bh, cover);
    else drawWhole(ctx, own, bx, by, bw, bh);
    ctx.restore();
    return;
  }
  if (entry && it.cell !== undefined) {
    const sheet = d.pictures.get(entry.url);
    if (sheet) {
      const { sx, sy } = atlasCell(entry.atlas, it.cell);
      ctx.save();
      rounded(ctx, x, y, w, h, radius);
      ctx.clip();
      drawFitted(ctx, sheet, sx, sy, entry.atlas.cellW, entry.atlas.cellH, bx, by, bw, bh, cover);
      ctx.restore();
    }
  }
};
