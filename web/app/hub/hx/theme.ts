import { useTheme } from '@kit';
import { useEffect } from 'react';

/** The page's theme for the design's tokens: data-hx-theme on <html> (the body's colour) and on the root. */
export const useHxTheme = (): 'light' | 'dark' => {
  const { resolved } = useTheme();
  useEffect(() => {
    const html = document.documentElement;
    html.dataset.hxTheme = resolved;
    return () => {
      delete html.dataset.hxTheme;
    };
  }, [resolved]);
  return resolved;
};
