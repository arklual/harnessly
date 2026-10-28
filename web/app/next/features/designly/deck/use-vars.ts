// CSS custom properties from a component's values, set on its element (no inline styles): positions of pins, popovers
// and the sheet's drag offset go through here, as `useSwatchColor` does in site-notes.tsx.
import { useLayoutEffect, useRef, type RefObject } from 'react';

export const useVars = <T extends HTMLElement>(vars: Record<string, string>): RefObject<T | null> => {
  const ref = useRef<T>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    for (const [name, value] of Object.entries(vars)) el.style.setProperty(name, value);
  });
  return ref;
};
