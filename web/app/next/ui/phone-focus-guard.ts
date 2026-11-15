// On the phone a sheet, a popover or the pane never raises the keyboard by itself: a text field inside one
// takes the focus only from the person's own tap on it. A field that an opening focuses (autoFocus, a focus() call)
// gives it back to its sheet at once, so the keyboard does not come up over what the person wanted to look at.
import { PHONE_QUERY } from '@/next/ui/use-media';

/** What raises the keyboard. */
const TEXT_FIELD =
  'textarea, [contenteditable="true"], input:not([type="checkbox"]):not([type="radio"]):not([type="button"]):not([type="submit"]):not([type="range"]):not([type="file"]):not([type="color"])';
/** Over the chat: a sheet, a popover or a dialog, the right pane. */
const OVER = '.hx-sheet, .hx-sheet-phone, [role="dialog"], [role="menu"], [role="listbox"], .hx-pane';
/** A tap or a key this recent is what moved the focus. */
const OWN_MS = 800;

/** Whether the focus came from the person: a tap on the field (or its label) or a key, just now. */
const ownFocus = (field: Element, tap: { el: Element | null; at: number }, keyAt: number, now: number) => {
  if (now - keyAt < OWN_MS) return true;
  if (!tap.el || now - tap.at > OWN_MS) return false;
  return tap.el === field || field.contains(tap.el) || tap.el.closest('label')?.contains(field) === true;
};

/** Installs the rule for the page; returns how to take it off. */
export const installPhoneFocusGuard = (): (() => void) => {
  const phone = window.matchMedia(PHONE_QUERY);
  const tap: { el: Element | null; at: number } = { el: null, at: 0 };
  let keyAt = 0;
  const onDown = (event: PointerEvent) => {
    tap.el = event.target instanceof Element ? event.target : null;
    tap.at = performance.now();
  };
  const onKey = () => {
    keyAt = performance.now();
  };
  const onFocus = (event: FocusEvent) => {
    if (!phone.matches) return;
    const field = event.target;
    if (!(field instanceof HTMLElement) || !field.matches(TEXT_FIELD)) return;
    const over = field.closest<HTMLElement>(OVER);
    if (!over || ownFocus(field, tap, keyAt, performance.now())) return;
    field.blur();
    // The sheet keeps the focus, so Esc and its trap still work.
    const holder = field.parentElement?.closest<HTMLElement>('[tabindex]') ?? over;
    holder.focus({ preventScroll: true });
  };
  document.addEventListener('pointerdown', onDown, true);
  document.addEventListener('keydown', onKey, true);
  document.addEventListener('focusin', onFocus, true);
  return () => {
    document.removeEventListener('pointerdown', onDown, true);
    document.removeEventListener('keydown', onKey, true);
    document.removeEventListener('focusin', onFocus, true);
  };
};
