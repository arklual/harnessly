/** Geometry of the floating preview window: pure, so dragging, resizing and restoring are testable. */

export interface IRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ISize {
  width: number;
  height: number;
}

export type TEdge = 'n' | 's' | 'e' | 'w' | 'ne' | 'nw' | 'se' | 'sw';

export const FLOATING_EDGES = [
  'n',
  's',
  'e',
  'w',
  'ne',
  'nw',
  'se',
  'sw',
] as const satisfies readonly TEdge[];

/** `restore` is the rect to return to while the window is maximised, null otherwise. */
export interface IFloatingLayout {
  rect: IRect;
  restore: IRect | null;
}

export const FLOATING_MIN: ISize = { width: 320, height: 240 };

const MAXIMISED_MARGIN = 16;
const DEFAULT_MARGIN = 24;
const DEFAULT_SIZE: ISize = { width: 720, height: 520 };

export const applyDrag = (rect: IRect, dx: number, dy: number): IRect => ({
  ...rect,
  x: rect.x + dx,
  y: rect.y + dy,
});

/** The dragged edges move up to the screen edge, the opposite ones stay put, and the size never goes below `min`. */
export const applyResize = (
  rect: IRect,
  edge: TEdge,
  dx: number,
  dy: number,
  min: ISize,
  viewport: ISize,
): IRect => {
  let { x, y, width, height } = rect;
  const right = rect.x + rect.width;
  const bottom = rect.y + rect.height;
  if (edge.includes('e')) width = Math.max(min.width, Math.min(right + dx, viewport.width) - rect.x);
  if (edge.includes('w')) {
    x = Math.min(right - min.width, Math.max(0, rect.x + dx));
    width = right - x;
  }
  if (edge.includes('s')) height = Math.max(min.height, Math.min(bottom + dy, viewport.height) - rect.y);
  if (edge.includes('n')) {
    y = Math.min(bottom - min.height, Math.max(0, rect.y + dy));
    height = bottom - y;
  }
  return { x, y, width, height };
};

const sameRect = (a: IRect, b: IRect): boolean =>
  a.x === b.x && a.y === b.y && a.width === b.width && a.height === b.height;

/** Layout during and after a drag or resize: a click that moved nothing keeps a maximised window maximised. */
export const gestureLayout = (start: IFloatingLayout, rect: IRect): IFloatingLayout =>
  sameRect(start.rect, rect) ? start : { rect, restore: null };

/** Fits the window into the screen; below the minimum size the minimum wins and the window sits at the top left. */
export const clampToViewport = (rect: IRect, viewport: ISize, min: ISize): IRect => {
  const width = Math.max(min.width, Math.min(rect.width, viewport.width));
  const height = Math.max(min.height, Math.min(rect.height, viewport.height));
  return {
    x: Math.max(0, Math.min(rect.x, viewport.width - width)),
    y: Math.max(0, Math.min(rect.y, viewport.height - height)),
    width,
    height,
  };
};

export const maximisedRect = (viewport: ISize): IRect =>
  clampToViewport(
    {
      x: MAXIMISED_MARGIN,
      y: MAXIMISED_MARGIN,
      width: viewport.width - 2 * MAXIMISED_MARGIN,
      height: viewport.height - 2 * MAXIMISED_MARGIN,
    },
    viewport,
    FLOATING_MIN,
  );

export const defaultFloatingRect = (viewport: ISize): IRect => {
  const width = Math.min(DEFAULT_SIZE.width, viewport.width - 2 * DEFAULT_MARGIN);
  const height = Math.min(DEFAULT_SIZE.height, viewport.height - 2 * DEFAULT_MARGIN);
  return clampToViewport(
    {
      x: viewport.width - width - DEFAULT_MARGIN,
      y: viewport.height - height - DEFAULT_MARGIN,
      width,
      height,
    },
    viewport,
    FLOATING_MIN,
  );
};

export const toggleMaximised = (layout: IFloatingLayout, viewport: ISize): IFloatingLayout =>
  layout.restore
    ? { rect: clampToViewport(layout.restore, viewport, FLOATING_MIN), restore: null }
    : { rect: maximisedRect(viewport), restore: layout.rect };

/** After the screen changed size: a maximised window fills the new screen, any other is brought back into view. */
export const fitFloatingLayout = (layout: IFloatingLayout, viewport: ISize): IFloatingLayout =>
  layout.restore
    ? { rect: maximisedRect(viewport), restore: clampToViewport(layout.restore, viewport, FLOATING_MIN) }
    : { rect: clampToViewport(layout.rect, viewport, FLOATING_MIN), restore: null };

export const floatingLayoutKey = (machine: string): string => `hl:preview-floating:${machine}`;

const isRect = (value: unknown): value is IRect => {
  if (typeof value !== 'object' || value === null) return false;
  const { x, y, width, height } = value as Record<string, unknown>;
  return [x, y, width, height].every((n) => typeof n === 'number' && Number.isFinite(n));
};

type TLayoutStorage = Pick<Storage, 'getItem' | 'setItem'>;

/** The layout this machine's window had last, fitted to the screen; anything unreadable starts over at the default. */
export const loadFloatingLayout = (
  storage: TLayoutStorage | null,
  machine: string,
  viewport: ISize,
): IFloatingLayout => {
  const initial: IFloatingLayout = { rect: defaultFloatingRect(viewport), restore: null };
  let saved: unknown;
  try {
    const raw = storage?.getItem(floatingLayoutKey(machine));
    if (!raw) return initial;
    saved = JSON.parse(raw);
  } catch {
    return initial;
  }
  if (typeof saved !== 'object' || saved === null) return initial;
  const { rect, restore } = saved as Record<string, unknown>;
  if (!isRect(rect) || (restore !== null && !isRect(restore))) return initial;
  return fitFloatingLayout({ rect, restore }, viewport);
};

export const saveFloatingLayout = (
  storage: TLayoutStorage | null,
  machine: string,
  layout: IFloatingLayout,
): void => {
  try {
    storage?.setItem(floatingLayoutKey(machine), JSON.stringify(layout));
  } catch {}
};
