// What a page says when the person presses «Изменить» or «Сгенерировать» on one of its pictures (`tk:image-act`, from the
// board's own script, internal/design/texts/client.js) and the host's word that the person may change pictures
// (`tk:images`). The page is foreign code: every field is checked and clipped here. No React.
import { readGen } from '@/next/features/designly/deck/bridge';
import type { IDeckGen } from '@/next/features/designly/deck/bridge';

export const IMAGE_ACT = { edit: 'edit', gen: 'gen' } as const;
export type TImageAct = (typeof IMAGE_ACT)[keyof typeof IMAGE_ACT];

/** Host → page: the page draws its buttons on pictures while this is on. */
export interface IImagesOn {
  type: 'tk:images';
  on: boolean;
}

export interface IImageAct {
  act: TImageAct;
  /** The block's `data-dc-tpl` and the board's fingerprint of it: a write carries both. */
  tpl: number;
  was: string;
  /** Its width over its height; 0 when unknown. */
  ratio: number;
  /** What it was drawn from (`data-gen-prompt`, `data-gen-job`). */
  gen: IDeckGen | null;
  /** The pictures it was changed from, oldest first (`data-gen-from`), as paths of the project. */
  from: readonly string[];
  /** Its picture as a path of the project (`assets/a.png`); '' for an empty place or a remote picture. */
  src: string;
  /** An empty place for a picture. */
  slot: boolean;
  /** The brief of an empty place (`data-image-prompt`). */
  brief: string;
  label: string;
  /** The board the page is, as a path of the project. */
  file: string;
  /** The block's box in the page's viewport, and the viewport's size: all CSS pixels of the page. */
  rect: { x: number; y: number; w: number; h: number };
  vw: number;
  vh: number;
}

const TPL_MAX = 1_000_000;
const COORD_MAX = 100_000;
const TRAIL_MAX = 12;
const PATH_RE = /^[\p{L}\p{N}_./-]{1,300}\.(?:png|jpe?g|webp)$/iu;
const BOARD_RE = /^[^/\\]{1,200}\.dc\.html$/;

const isNum = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const clip = (v: unknown, max: number) => (typeof v === 'string' ? v.slice(0, max) : '');

/** A path of the project, plain and relative, to a picture; '' for anything else. */
export const pictureOf = (v: unknown): string => {
  const path = clip(v, 301);
  if (!PATH_RE.test(path) || path.startsWith('/') || path.startsWith('.')) return '';
  return path.split('/').every((part) => part !== '' && part !== '.' && part !== '..') ? path : '';
};

/** The board file of a path of the project: a board at the root only; '' for anything else. */
export const boardOfPath = (v: unknown): string => {
  const file = clip(v, 201);
  return BOARD_RE.test(file) ? file : '';
};

export const readImageAct = (m: Record<string, unknown>): IImageAct | null => {
  if (m.type !== 'tk:image-act') return null;
  const act = m.act === IMAGE_ACT.edit || m.act === IMAGE_ACT.gen ? m.act : null;
  const tpl = m.tpl;
  const r = Array.isArray(m.rect) && m.rect.length === 4 ? (m.rect as unknown[]) : null;
  if (act === null || typeof tpl !== 'number' || !Number.isInteger(tpl) || tpl < 0 || tpl > TPL_MAX)
    return null;
  if (typeof m.was !== 'string' || m.was === '' || r === null || !r.every(isNum)) return null;
  if (!isNum(m.vw) || !isNum(m.vh) || m.vw <= 0 || m.vh <= 0) return null;
  const [x, y, w, h] = (r as number[]).map((v) => Math.max(-COORD_MAX, Math.min(COORD_MAX, v)));
  if ((w as number) < 0 || (h as number) < 0) return null;
  const trail = clip(m.from, 4000)
    .split(/\s+/)
    .map(pictureOf)
    .filter((path) => path !== '')
    .slice(-TRAIL_MAX);
  return {
    act,
    tpl,
    was: clip(m.was, 64),
    ratio: isNum(m.ratio) && m.ratio > 0 && m.ratio <= 1000 ? m.ratio : 0,
    gen: readGen(m.gen),
    from: trail,
    src: pictureOf(m.src),
    slot: m.slot === true,
    brief: clip(m.brief, 1000),
    label: clip(m.label, 120),
    file: boardOfPath(m.file),
    rect: { x: x as number, y: y as number, w: w as number, h: h as number },
    vw: Math.min(m.vw, COORD_MAX),
    vh: Math.min(m.vh, COORD_MAX),
  };
};

/** Where the block is on the screen: the frame's box and the page's viewport tell the scale. */
export interface IScreenBox {
  left: number;
  top: number;
  width: number;
  height: number;
}

export const screenBoxOf = (
  frame: { left: number; top: number; width: number },
  act: Pick<IImageAct, 'rect' | 'vw'>,
): IScreenBox => {
  const k = act.vw > 0 ? frame.width / act.vw : 1;
  return {
    left: frame.left + act.rect.x * k,
    top: frame.top + act.rect.y * k,
    width: act.rect.w * k,
    height: act.rect.h * k,
  };
};

/**
 * The scale a frame shows its page at (a deck's slide is the page shrunk to the stage, a canvas zooms it): the page draws
 * its buttons at the inverse, so they have one size on the screen. 1 when the frame is not laid out.
 */
export const frameScale = (frame: HTMLElement | null): number => {
  if (frame === null || frame.offsetWidth <= 0) return 1;
  const k = frame.getBoundingClientRect().width / frame.offsetWidth;
  return Number.isFinite(k) && k >= 0.05 && k <= 8 ? Math.round(k * 1000) / 1000 : 1;
};
