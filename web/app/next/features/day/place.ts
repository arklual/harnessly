// Where a thing stands on the strip is a number the data gives: it goes to the element as CSS variables through a ref
// (no inline styles), and the stylesheet turns them into a position.
export const cssVars =
  (values: Record<string, string>) =>
  (el: HTMLElement | null): void => {
    if (!el) return;
    for (const [name, value] of Object.entries(values)) el.style.setProperty(name, value);
  };
