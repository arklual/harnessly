import { useSyncExternalStore } from 'react';

// The page's colour theme as a small store outside React: the chosen mode lives in localStorage, "system" follows the
// operating system, and the resolved theme is written to <html data-theme> where the tokens read it.

export type ThemeMode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export interface ThemeState {
  mode: ThemeMode;
  resolved: ResolvedTheme;
}

const MODES: readonly ThemeMode[] = ['light', 'dark', 'system'];
let key = 'theme';
let state: ThemeState = { mode: 'system', resolved: 'light' };
const listeners = new Set<() => void>();
let media: MediaQueryList | null = null;

function osTheme(): ResolvedTheme {
  return media?.matches ? 'dark' : 'light';
}

function publish(mode: ThemeMode, force = false) {
  const resolved = mode === 'system' ? osTheme() : mode;
  if (!force && state.mode === mode && state.resolved === resolved) return;
  state = { mode, resolved };
  const html = document.documentElement;
  html.dataset.theme = resolved;
  html.style.colorScheme = resolved;
  listeners.forEach((fn) => fn());
}

/** Reads the stored mode and starts following the system; call once before the first render. */
export function startTheme(opts: { storageKey?: string; fallback?: ThemeMode } = {}) {
  key = opts.storageKey ?? key;
  media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-color-scheme: dark)') : null;
  media?.addEventListener('change', () => state.mode === 'system' && publish('system'));
  let stored: string | null = null;
  try {
    stored = localStorage.getItem(key);
  } catch {
    // blocked storage: the fallback applies
  }
  publish(MODES.find((m) => m === stored) ?? opts.fallback ?? 'system', true);
}

export function setThemeMode(mode: ThemeMode) {
  try {
    localStorage.setItem(key, mode);
  } catch {
    // kept for this page only
  }
  publish(mode);
}

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => listeners.delete(fn);
};
const snapshot = () => state;

/** The current theme and a setter, re-rendering on change. */
export function useTheme(): ThemeState & { setMode: (mode: ThemeMode) => void } {
  const s = useSyncExternalStore(subscribe, snapshot, snapshot);
  return { ...s, setMode: setThemeMode };
}
