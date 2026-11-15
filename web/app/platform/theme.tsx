// The theme switch the pages use (the surface of next-themes): light, dark or system, stored per browser,
// applied as the `dark` class and `color-scheme` on <html>; and the palette over it (graphite, midnight, forest…),
// stored the same way and applied as `data-palette` (styles/palettes.css). A change made from a click spreads from
// that point as a circle (a view transition) where the browser can and the person did not ask for less motion.
import { notePrefWrite } from '@/next/core/pref-write';
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

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
  systemTheme: 'light' | 'dark';
  themes: TTheme[];
  setTheme: (theme: TTheme | string, from?: IChangeOrigin) => void;
  palette: TPalette;
  setPalette: (palette: TPalette, from?: IChangeOrigin) => void;
}

const KEY = 'hl-theme';
const PALETTE_KEY = 'hl-palette';
/** As long as hx-look-in in styles/palettes.css. */
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

const paint = (mode: 'light' | 'dark', palette: TPalette) => {
  const root = document.documentElement;
  root.classList.toggle('dark', mode === 'dark');
  root.dataset.theme = mode;
  root.dataset.palette = palette;
  root.style.colorScheme = mode;
};

type TViewTransition = { finished: Promise<void> };
type TTransitionDocument = Document & { startViewTransition?: (change: () => void) => TViewTransition };

/** Repaints with the circle spreading from `from` over the old look; at once without a point, the browser's support,
 *  or when the person asked the system for less motion. */
const repaint = (change: () => void, from: IChangeOrigin | undefined) => {
  const doc = document as TTransitionDocument;
  const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  if (!from || !doc.startViewTransition || still) {
    change();
    return;
  }
  const root = document.documentElement;
  const reach = Math.hypot(
    Math.max(from.x, window.innerWidth - from.x),
    Math.max(from.y, window.innerHeight - from.y),
  );
  root.style.setProperty('--hx-look-x', `${from.x}px`);
  root.style.setProperty('--hx-look-y', `${from.y}px`);
  root.style.setProperty('--hx-look-r', `${Math.ceil(reach)}px`);
  root.dataset.look = 'spread';
  const done = () => delete root.dataset.look;
  const timer = window.setTimeout(done, SPREAD_MS + 200);
  void doc.startViewTransition(change).finished.finally(() => {
    window.clearTimeout(timer);
    done();
  });
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
  const resolvedTheme = theme === 'system' ? systemTheme : theme;
  useEffect(() => paint(resolvedTheme, palette), [resolvedTheme, palette]);
  // The page is painted inside the transition, before React's next commit: the circle shows the new look at once.
  const setTheme = useCallback(
    (next: TTheme | string, from?: IChangeOrigin) => {
      const value: TTheme = next === 'light' || next === 'dark' ? next : 'system';
      keep(KEY, value);
      repaint(() => {
        paint(value === 'system' ? systemTheme : value, palette);
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
      systemTheme,
      themes: ['light', 'dark', 'system'] as TTheme[],
      setTheme,
      palette,
      setPalette,
    }),
    [theme, resolvedTheme, systemTheme, setTheme, palette, setPalette],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export const useTheme = (): IThemeContext => {
  const value = useContext(Ctx);
  if (!value) throw new Error('useTheme outside ThemeProvider');
  return value;
};
