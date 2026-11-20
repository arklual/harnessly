// One card over the field at a time: a popover, a menu or a hover card that opens tells the page, and every other one
// that is open closes (the limits' card, the context ring's, the model pill's panel, the quick voice settings). Each
// passes its own way to close; nothing here knows what the others are.
import { useEffect, useEffectEvent, useId } from 'react';

const OPENED = 'hx-popover-opened';

/** While `open`: announces itself once, and closes when another one announces itself. */
export const useOnePopover = (open: boolean, close: () => void): void => {
  const id = useId();
  const closeNow = useEffectEvent(close);
  useEffect(() => {
    if (!open) return;
    const onOther = (event: Event) => {
      if ((event as CustomEvent<string>).detail !== id) closeNow();
    };
    window.addEventListener(OPENED, onOther);
    window.dispatchEvent(new CustomEvent(OPENED, { detail: id }));
    return () => window.removeEventListener(OPENED, onOther);
  }, [open, id]);
};
