// Custom properties of an element, set through its style object (the CSS reads them: `--v`, `--c`, `--x`…). The pane
// takes numbers from the machine at run time, and a stylesheet cannot: no inline style attribute, only these.
import type { TVars } from '@/next/features/pane/machine/charts';

/** A callback ref that gives the element these properties on every render. */
export const vars =
  (values: TVars) =>
  (element: HTMLElement | SVGElement | null): void => {
    if (!element) return;
    for (const [name, value] of Object.entries(values)) element.style.setProperty(`--${name}`, String(value));
  };
