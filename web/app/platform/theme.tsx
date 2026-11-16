// The theme switch the pages use (the surface of next-themes): light, dark or system, stored per browser,
// applied as the `dark` class and `color-scheme` on <html>; and the palette over it (graphite, midnight, forest…),
// stored the same way and applied as `data-palette` (styles/palettes.css). A change made from a click spreads from
// that point as a circle (a view transition) where the browser can and the person did not ask for less motion.
import { notePrefWrite } from '@/next/core/pref-write';
import { flushSync } from 'react-dom';
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from 'react';

type TTheme = 'light' | 'dark' | 'system';

export const PALETTES = ['graphite', 'midnight', 'forest', 'sand', 'rose', 'lagoon', 'contrast'] as const;
export type TPalette = (typeof PALETTES)[number];

/** Where the change starts on screen: the middle of what was clicked. */
export interface IChangeOrigin {
  x: number;
  y: number;
}

interface IThemeContext {
  theme: TTheme;
  resolvedTheme: 'light' | 'dark';
  /** The person's own choice resolved, whatever a look of theirs forces (resolvedTheme is what the page wears). */
  naturalTheme: 'light' | 'dark';
  systemTheme: 'light' | 'dark';
  themes: TTheme[];
  setTheme: (theme: TTheme | string, from?: IChangeOrigin) => void;
  palette: TPalette;
  setPalette: (palette: TPalette, from?: IChangeOrigin) => void;
}

const KEY = 'hl-theme';
const PALETTE_KEY = 'hl-palette';
/** How long the circle spreads. */
const SPREAD_MS = 620;
const media = () =>
  typeof window !== 'undefined' ? window.matchMedia('(prefers-color-scheme: dark)') : null;
const readStored = (): TTheme => {
  try {
    const value = window.localStorage.getItem(KEY);
    return value === 'light' || value === 'dark' || value === 'system' ? value : 'system';
  } catch {
    return 'system';
  }
};

const readPalette = (): TPalette => {
  try {
    const value = window.localStorage.getItem(PALETTE_KEY);
    return PALETTES.find((palette) => palette === value) ?? 'graphite';
  } catch {
    return 'graphite';
  }
};

const keep = (key: string, value: string) => {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // private mode: the choice lasts for this page only
  }
  notePrefWrite(key);
};

/** Writes only what differs: a write of the same value still invalidated the whole document's styles. */
const paint = (mode: 'light' | 'dark', palette: TPalette) => {
  const root = document.documentElement;
  if (root.classList.contains('dark') !== (mode === 'dark')) root.classList.toggle('dark', mode === 'dark');
  if (root.dataset.theme !== mode) root.dataset.theme = mode;
  if (root.dataset.palette !== palette) root.dataset.palette = palette;
  if (root.style.colorScheme !== mode) root.style.colorScheme = mode;
};

type TViewTransition = { finished: Promise<void>; ready: Promise<void> };
type TTransitionDocument = Document & { startViewTransition?: (change: () => void) => TViewTransition };

/** Repaints with the circle spreading from `from` over the old look; at once without a point, the browser's support,
 *  or when the person asked the system for less motion.
 *
 *  The look is the same as before, only cheaper: while it changes, html[data-look-switch] turns every CSS
 *  transition off (palettes.css). Without that the new tokens started a colour transition on each of the page's
 *  thousand-odd elements, and every frame of the circle recalculated their styles on the main thread (≈ 60–80 ms a
 *  frame at CPU ×4). React's render of the new look is flushed inside the change, so the browser takes its one new
 *  snapshot after all the work and the circle itself runs on the compositor. */
const repaint = (change: () => void, from: IChangeOrigin | undefined) => {
  const doc = document as TTransitionDocument;
  const root = document.documentElement;
  const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  // Set in the same style pass as the new tokens: the transitions are off for the change itself, at no extra pass.
  const apply = () => {
    root.dataset.lookSwitch = '';
    flushSync(change);
  };
  if (!from || !doc.startViewTransition || still) {
    apply();
    // Two frames on, so the styles without transitions are the ones the change was painted with.
    requestAnimationFrame(() => requestAnimationFrame(() => delete root.dataset.lookSwitch));
    return;
  }
  const reach = Math.hypot(
    Math.max(from.x, window.innerWidth - from.x),
    Math.max(from.y, window.innerHeight - from.y),
  );
  // The circle is animated on the new snapshot itself (the Web Animations API on the pseudo-element): no style of the
  // document changes for it, where a custom property on <html> or a <style> added for it recalculated every element.
  const circle = (r: number) => `circle(${r}px at ${from.x}px ${from.y}px)`;
  const ended = { on: false };
  const done = () => {
    if (ended.on) return;
    ended.on = true;
    // The transitions come back when the page is idle: that pass touches every element, after the circle.
    const back = () => delete root.dataset.lookSwitch;
    if (typeof window.requestIdleCallback === 'function') window.requestIdleCallback(back, { timeout: 1000 });
    else setTimeout(back, 200);
  };
  const transition = doc.startViewTransition(apply);
  void transition.ready
    .then(() => {
      root.animate(
        { clipPath: [circle(0), circle(Math.ceil(reach))] },
        {
          duration: SPREAD_MS,
          easing: 'cubic-bezier(0.22, 1, 0.36, 1)',
          pseudoElement: '::view-transition-new(root)',
        },
      );
    })
    .catch(() => undefined);
  void transition.finished.finally(done);
};

/** The light or the dark a look of the person's own asks for (next/features/looks): it wins over the choice above while
 *  that look is on, and the choice comes back when it goes. null: no look asks. */
let forcedMode: 'light' | 'dark' | null = null;
const forcedListeners = new Set<() => void>();
export const setForcedMode = (mode: 'light' | 'dark' | null): void => {
  if (forcedMode === mode) return;
  forcedMode = mode;
  forcedListeners.forEach((fn) => fn());
};
const subscribeForced = (fn: () => void) => {
  forcedListeners.add(fn);
  return () => forcedListeners.delete(fn);
};

const Ctx = createContext<IThemeContext | null>(null);

export function ThemeProvider({
  children,
}: {
  children: ReactNode;
  attribute?: string;
  defaultTheme?: string;
  enableSystem?: boolean;
  disableTransitionOnChange?: boolean;
}) {
  const [theme, setThemeState] = useState<TTheme>(readStored);
  const [palette, setPaletteState] = useState<TPalette>(readPalette);
  const [systemTheme, setSystemTheme] = useState<'light' | 'dark'>(() =>
    media()?.matches ? 'dark' : 'light',
  );
  useEffect(() => {
    const m = media();
    if (!m) return;
    const on = () => setSystemTheme(m.matches ? 'dark' : 'light');
    m.addEventListener('change', on);
    return () => m.removeEventListener('change', on);
  }, []);
  // The person's settings brought another device's theme or palette (next/stores/synced-prefs.ts).
  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === KEY) setThemeState(readStored());
      if (event.key === PALETTE_KEY) setPaletteState(readPalette());
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);
  const forced = useSyncExternalStore(
    subscribeForced,
    () => forcedMode,
    () => null,
  );
  const naturalTheme = theme === 'system' ? systemTheme : theme;
  const resolvedTheme = forced ?? naturalTheme;
  useEffect(() => paint(resolvedTheme, palette), [resolvedTheme, palette]);
  // The page is painted inside the transition, before React's next commit: the circle shows the new look at once.
  const setTheme = useCallback(
    (next: TTheme | string, from?: IChangeOrigin) => {
      const value: TTheme = next === 'light' || next === 'dark' ? next : 'system';
      keep(KEY, value);
      repaint(() => {
        paint(forcedMode ?? (value === 'system' ? systemTheme : value), palette);
        setThemeState(value);
      }, from);
    },
    [systemTheme, palette],
  );
  const setPalette = useCallback(
    (next: TPalette, from?: IChangeOrigin) => {
      keep(PALETTE_KEY, next);
      repaint(() => {
        paint(resolvedTheme, next);
        setPaletteState(next);
      }, from);
    },
    [resolvedTheme],
  );
  const value = useMemo(
    () => ({
      theme,
      resolvedTheme,
      naturalTheme,
      systemTheme,
      themes: ['light', 'dark', 'system'] as TTheme[],
      setTheme,
      palette,
      setPalette,
    }),
    [theme, resolvedTheme, naturalTheme, systemTheme, setTheme, palette, setPalette],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export const useTheme = (): IThemeContext => {
  const value = useContext(Ctx);
  if (!value) throw new Error('useTheme outside ThemeProvider');
  return value;
};
