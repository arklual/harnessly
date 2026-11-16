// Custom properties of an element from its values, set through its node (not a style attribute): a value the CSS
// reads (a position, a colour, a fill) without inline styles. Only what changed is written.
import { useCallback, useLayoutEffect, useRef } from 'react';

export const useCssVars = <T extends HTMLElement>(vars: Readonly<Record<`--${string}`, string>>) => {
  const node = useRef<T | null>(null);
  const key = JSON.stringify(vars);
  useLayoutEffect(() => {
    const el = node.current;
    if (!el) return;
    for (const [name, value] of Object.entries(JSON.parse(key) as Record<string, string>))
      if (el.style.getPropertyValue(name) !== value) el.style.setProperty(name, value);
  }, [key]);
  return useCallback(
    (el: T | null) => {
      node.current = el;
      if (!el) return;
      for (const [name, value] of Object.entries(JSON.parse(key) as Record<string, string>))
        el.style.setProperty(name, value);
    },
    [key],
  );
};
