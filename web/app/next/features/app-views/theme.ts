// The theme a view is given: the spec's --t-* tokens, read from the page's own (--hx-*) at the moment, plus the mode.
import type { IViewContext } from '@/next/features/app-views/bridge';

/** What a view's --t-* token takes from the page. */
export const THEME_TOKENS = {
  '--t-bg': '--hx-bg-pane',
  '--t-surface': '--hx-bg-raised',
  '--t-surface-hover': '--hx-bg-hover',
  '--t-text': '--hx-fg-1',
  '--t-text-secondary': '--hx-fg-2',
  '--t-text-muted': '--hx-fg-3',
  '--t-border': '--hx-border',
  '--t-border-strong': '--hx-border-strong',
  '--t-accent': '--hx-accent',
  '--t-danger': '--hx-danger',
  '--t-success': '--hx-success',
} as const;

export const viewTheme = (
  mode: 'light' | 'dark',
  root: Element | null = document.documentElement,
): IViewContext['theme'] => {
  const tokens: Record<string, string> = {};
  if (root) {
    const style = getComputedStyle(root);
    for (const [name, source] of Object.entries(THEME_TOKENS)) {
      const value = style.getPropertyValue(source).trim();
      if (value !== '') tokens[name] = value;
    }
  }
  return { mode, tokens };
};
