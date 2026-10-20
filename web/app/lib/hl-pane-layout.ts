type TLayoutStorage = Pick<Storage, 'getItem' | 'setItem'>;

/** How the right panel stood in a conversation when the person last left it. */
export interface IPaneLayout {
  open: boolean;
  width: number;
  maximised: boolean;
}

export const PANE_LAYOUT_KEY = 'hl:pane-layout';
/** Conversations whose panel is remembered; the ones not opened for the longest are forgotten first. */
export const PANE_LAYOUT_LIMIT = 300;

const WIDTH_MIN = 200;
const WIDTH_MAX = 4000;

const isLayout = (value: unknown): value is IPaneLayout => {
  if (typeof value !== 'object' || value === null) return false;
  const { open, width, maximised } = value as Record<string, unknown>;
  return (
    typeof open === 'boolean' &&
    typeof maximised === 'boolean' &&
    typeof width === 'number' &&
    width >= WIDTH_MIN &&
    width <= WIDTH_MAX
  );
};

/** Saved as pairs, the conversation used last at the end. */
const readAll = (storage: TLayoutStorage | null): [string, IPaneLayout][] => {
  try {
    const saved: unknown = JSON.parse(storage?.getItem(PANE_LAYOUT_KEY) ?? 'null');
    if (!Array.isArray(saved)) return [];
    return saved.filter(
      (pair): pair is [string, IPaneLayout] =>
        Array.isArray(pair) && typeof pair[0] === 'string' && isLayout(pair[1]),
    );
  } catch {
    return [];
  }
};

/** Null for a conversation whose panel the person has not touched yet. */
export const readPaneLayout = (storage: TLayoutStorage | null, key: string): IPaneLayout | null =>
  readAll(storage).find(([saved]) => saved === key)?.[1] ?? null;

export const savePaneLayout = (storage: TLayoutStorage | null, key: string, layout: IPaneLayout): void => {
  if (!storage || !isLayout(layout)) return;
  const all = readAll(storage);
  const last = all[all.length - 1];
  if (
    last?.[0] === key &&
    last[1].open === layout.open &&
    last[1].width === layout.width &&
    last[1].maximised === layout.maximised
  )
    return;
  const next = [...all.filter(([saved]) => saved !== key), [key, layout] as const].slice(-PANE_LAYOUT_LIMIT);
  try {
    storage.setItem(PANE_LAYOUT_KEY, JSON.stringify(next));
  } catch {}
};

export const browserLayoutStorage = (): TLayoutStorage | null => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};
