// The theme switch the pages use (the surface of next-themes): light, dark or system, stored per browser,
// applied as the `dark` class and `color-scheme` on <html>.
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

type TTheme = 'light' | 'dark' | 'system';
interface IThemeContext {
  theme: TTheme;
  resolvedTheme: 'light' | 'dark';
  systemTheme: 'light' | 'dark';
  themes: TTheme[];
  setTheme: (theme: TTheme | string) => void;
}

const KEY = 'hl-theme';
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
  const resolvedTheme = theme === 'system' ? systemTheme : theme;
  useEffect(() => {
    const root = document.documentElement;
    root.classList.toggle('dark', resolvedTheme === 'dark');
    root.dataset.theme = resolvedTheme;
    root.style.colorScheme = resolvedTheme;
  }, [resolvedTheme]);
  const setTheme = useCallback((next: TTheme | string) => {
    const value: TTheme = next === 'light' || next === 'dark' ? next : 'system';
    setThemeState(value);
    try {
      window.localStorage.setItem(KEY, value);
    } catch {
      // private mode: the choice lasts for this page only
    }
  }, []);
  const value = useMemo(
    () => ({ theme, resolvedTheme, systemTheme, themes: ['light', 'dark', 'system'] as TTheme[], setTheme }),
    [theme, resolvedTheme, systemTheme, setTheme],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export const useTheme = (): IThemeContext => {
  const value = useContext(Ctx);
  if (!value) throw new Error('useTheme outside ThemeProvider');
  return value;
};
