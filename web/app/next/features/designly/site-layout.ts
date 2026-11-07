// Where the chat of each open design stood, as the person left it: the field, the column at an edge and how wide, or
// the window and where, and where the folded one opens again. Going to a chat and back to the design finds it so. This
// browser's only: localStorage, the most recent designs kept. Picking notes is a moment's mode: it comes back as the
// field.
import { SITE_MODES, type TSiteMode } from '@/next/features/designly/site-chat';
import type { IFloat, TDock } from '@/next/features/designly/site-drag';

export interface ISiteLayout {
  mode: TSiteMode;
  min: boolean;
  dock: TDock;
  chatW: number;
  float: IFloat | null;
  opened: TSiteMode;
  /** The person placed the chat by hand (picked a mode, dragged it): only then the kept place wins over the default. */
  chosen: boolean;
}

const KEY = 'hx-site-layouts';
/** More designs than this are not a working set; the least recent are forgotten. */
export const MAX_LAYOUTS = 50;

const isNumber = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const isMode = (v: unknown): v is TSiteMode => SITE_MODES.some((mode) => mode === v);
const isDock = (v: unknown): v is TDock => v === 'left' || v === 'right';
const isFloat = (v: unknown): v is IFloat => {
  if (typeof v !== 'object' || v === null) return false;
  const f = v as Record<string, unknown>;
  return isNumber(f.x) && isNumber(f.y) && isNumber(f.w) && isNumber(f.h);
};

/** One kept layout, or null when it is not one (an older shape, a hand edit). */
export const parseLayout = (raw: unknown): ISiteLayout | null => {
  if (typeof raw !== 'object' || raw === null) return null;
  const l = raw as Record<string, unknown>;
  if (!isMode(l.mode) || typeof l.min !== 'boolean' || !isDock(l.dock) || !isNumber(l.chatW)) return null;
  if (!isMode(l.opened) || !(l.float === null || isFloat(l.float))) return null;
  return {
    mode: l.mode === 'notes' ? 'island' : l.mode,
    min: l.min,
    dock: l.dock,
    chatW: l.chatW,
    float: l.float,
    opened: l.opened,
    chosen: l.chosen === true,
  };
};

/** The kept layouts, most recent last. */
export const parseLayouts = (raw: string | null): Map<string, ISiteLayout> => {
  const out = new Map<string, ISiteLayout>();
  if (raw === null) return out;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== 'object' || parsed === null) return out;
    for (const [key, value] of Object.entries(parsed)) {
      const layout = parseLayout(value);
      if (layout) out.set(key, layout);
    }
  } catch {
    // A broken entry: nothing was kept.
  }
  return out;
};

/** The layouts with this one put last, the oldest beyond MAX_LAYOUTS dropped. */
export const withLayout = (
  kept: ReadonlyMap<string, ISiteLayout>,
  key: string,
  layout: ISiteLayout,
): Map<string, ISiteLayout> => {
  const next = new Map(kept);
  next.delete(key);
  next.set(key, layout);
  while (next.size > MAX_LAYOUTS) next.delete(next.keys().next().value as string);
  return next;
};

const read = (): Map<string, ISiteLayout> => {
  try {
    return parseLayouts(localStorage.getItem(KEY));
  } catch {
    return new Map();
  }
};

export const keptLayout = (key: string): ISiteLayout | null => read().get(key) ?? null;

export const keepLayout = (key: string, layout: ISiteLayout) => {
  try {
    localStorage.setItem(KEY, JSON.stringify(Object.fromEntries(withLayout(read(), key, layout))));
  } catch {
    // Blocked or full storage: the layout holds while the design is open.
  }
};
