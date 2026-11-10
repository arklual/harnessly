import type { WindowState } from './config';

export interface Rect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface DisplayInfo {
  id: number;
  workArea: Rect;
}

const MIN_OVERLAP = 50;

export const isVisibleOnAnyDisplay = (bounds: Rect, displays: readonly DisplayInfo[]): boolean =>
  displays.some(({ workArea: a }) => {
    const overlapX = Math.max(0, Math.min(bounds.x + bounds.width, a.x + a.width) - Math.max(bounds.x, a.x));
    const overlapY = Math.max(0, Math.min(bounds.y + bounds.height, a.y + a.height) - Math.max(bounds.y, a.y));
    return overlapX >= MIN_OVERLAP && overlapY >= MIN_OVERLAP;
  });

/** Saved position when it is still on a screen, else centred on the saved display, else left to the OS. */
export const restorePosition = (
  saved: WindowState,
  displays: readonly DisplayInfo[],
): { x?: number; y?: number } => {
  if (saved.x != null && saved.y != null) {
    const bounds = { x: saved.x, y: saved.y, width: saved.width, height: saved.height };
    if (isVisibleOnAnyDisplay(bounds, displays)) return { x: saved.x, y: saved.y };
  }
  const target = saved.displayId != null ? displays.find((d) => d.id === saved.displayId) : undefined;
  if (!target) return {};
  const { x, y, width, height } = target.workArea;
  return { x: x + Math.floor((width - saved.width) / 2), y: y + Math.floor((height - saved.height) / 2) };
};

export const cascadeOffset = (openWindows: number): number => (openWindows === 0 ? 0 : (openWindows * 32) % 200);
