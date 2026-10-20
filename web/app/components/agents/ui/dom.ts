/** Small DOM helpers of the Hl UI primitives. No framework or domain imports. */

interface IStoppable {
  stopPropagation: () => void;
  preventDefault: () => void;
}

const stop = (event: IStoppable) => event.stopPropagation();

/**
 * Portalled surfaces are still React children of their trigger, so React events would bubble to the
 * trigger's ancestors (a clickable row, a composer handling Enter). Spread on scrims and backdrops.
 */
export const STOP_REACT_BUBBLING = {
  onClick: stop,
  onPointerDown: stop,
  onKeyDown: stop,
  onContextMenu: (event: IStoppable) => {
    event.preventDefault();
    event.stopPropagation();
  },
} as const;

type TClassValue = string | false | null | undefined;

/** Joins class names. Deliberately not a Tailwind merger: primitives only use tw-ui-* classes. */
export const cx = (...values: TClassValue[]): string => values.filter(Boolean).join(' ');

const TABBABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'textarea:not([disabled])',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

const isVisible = (element: HTMLElement) => element.getClientRects().length > 0;

/** Elements Tab can reach inside `root`, in DOM order. */
export const tabbables = (root: HTMLElement): HTMLElement[] =>
  [...root.querySelectorAll<HTMLElement>(TABBABLE)].filter(
    (element) =>
      !element.closest('[inert]') && element.getAttribute('aria-hidden') !== 'true' && isVisible(element),
  );

const FOCUS_OPTIONS = { preventScroll: true } as const;

/** Focuses the first element Tab reaches, or the container itself (it must have tabIndex -1). */
export const focusFirst = (root: HTMLElement): void => {
  const [first] = tabbables(root);
  (first ?? root).focus(FOCUS_OPTIONS);
};

export const focusElement = (element: HTMLElement | null | undefined): void => {
  element?.focus(FOCUS_OPTIONS);
};

/** Keeps Tab and Shift+Tab inside `root`. Returns true when the key was handled. */
export const trapTab = (
  root: HTMLElement,
  event: KeyboardEvent | { key: string; shiftKey: boolean; preventDefault: () => void },
): boolean => {
  if (event.key !== 'Tab') return false;
  const items = tabbables(root);
  if (!items.length) {
    event.preventDefault();
    root.focus(FOCUS_OPTIONS);
    return true;
  }
  const first = items[0];
  const last = items[items.length - 1];
  const active = document.activeElement;
  if (event.shiftKey && (active === first || active === root)) {
    event.preventDefault();
    last.focus(FOCUS_OPTIONS);
    return true;
  }
  if (!event.shiftKey && active === last) {
    event.preventDefault();
    first.focus(FOCUS_OPTIONS);
    return true;
  }
  return false;
};

/** Keeps the active row of a scrolling list in view without jumping the page. */
export const scrollIntoViewIfNeeded = (element: HTMLElement | null): void => {
  element?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
};
