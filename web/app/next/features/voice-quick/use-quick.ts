// What opens the quick voice settings from a button: a right click (two fingers on a trackpad), Shift+F10 or the menu
// key on the focused button, and on a touch screen a long press (~450 ms, with a light buzz where the browser has one).
// A plain tap or click still does what the button always did.
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
  type PointerEvent,
} from 'react';

/** How long a finger stays down before it counts as «more». */
export const LONG_PRESS_MS = 450;
/** The finger may wobble this far (px) and it is still a press. */
const SLOP_PX = 10;
/** The click that ends a long press is swallowed for this long. */
const CLICK_GUARD_MS = 800;
/** A keyboard's menu key sends both a key and a contextmenu event: the second one within this time is the same press. */
const SAME_PRESS_MS = 120;
const BUZZ_MS = 12;

export interface IQuickButtonProps {
  onContextMenu: (event: MouseEvent<HTMLElement>) => void;
  onKeyDown: (event: KeyboardEvent<HTMLElement>) => void;
  onPointerDown: (event: PointerEvent<HTMLElement>) => void;
  onPointerMove: (event: PointerEvent<HTMLElement>) => void;
  onPointerUp: () => void;
  onPointerCancel: () => void;
  onClickCapture: (event: MouseEvent<HTMLElement>) => void;
  'aria-haspopup': 'dialog';
  'aria-expanded': boolean;
  'data-quick': '';
}

export interface IQuick {
  open: boolean;
  setOpen: (open: boolean) => void;
  /** The list shown in place of the settings (a choice opened with its row), null for the settings themselves. */
  sub: string | null;
  setSub: (sub: string | null) => void;
  /** The button the panel hangs on; it also gets the focus back. */
  anchor: HTMLElement | null;
  setAnchor: (el: HTMLElement | null) => void;
  /** Spread on the button. */
  props: IQuickButtonProps;
}

export const useQuick = (): IQuick => {
  const [open, setOpenNow] = useState(false);
  const [sub, setSub] = useState<string | null>(null);
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const press = useRef<{ x: number; y: number; timer: ReturnType<typeof setTimeout> } | null>(null);
  const touched = useRef(false);
  const swallow = useRef(false);
  const swallowTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const keyAt = useRef(0);

  // Every opening starts at the settings, not at the list the last one ended in.
  const setOpen = useCallback((next: boolean) => {
    setOpenNow(next);
    setSub(null);
  }, []);
  const toggle = useCallback(() => {
    setOpenNow((was) => !was);
    setSub(null);
  }, []);
  const cancelPress = useCallback(() => {
    if (press.current) clearTimeout(press.current.timer);
    press.current = null;
  }, []);
  useEffect(
    () => () => {
      cancelPress();
      clearTimeout(swallowTimer.current);
    },
    [cancelPress],
  );

  const props: IQuickButtonProps = {
    onContextMenu: (event) => {
      // The browser's own menu never shows over these buttons.
      event.preventDefault();
      // A touch screen's long press sends this event too, after the timer opened the panel: not a second press.
      if (touched.current) return;
      if (Date.now() - keyAt.current < SAME_PRESS_MS) return;
      toggle();
    },
    onKeyDown: (event) => {
      if (event.key !== 'ContextMenu' && !(event.key === 'F10' && event.shiftKey)) return;
      event.preventDefault();
      keyAt.current = Date.now();
      toggle();
    },
    onPointerDown: (event) => {
      touched.current = event.pointerType === 'touch' || event.pointerType === 'pen';
      cancelPress();
      if (!touched.current || !event.isPrimary) return;
      const x = event.clientX;
      const y = event.clientY;
      press.current = {
        x,
        y,
        timer: setTimeout(() => {
          press.current = null;
          swallow.current = true;
          clearTimeout(swallowTimer.current);
          swallowTimer.current = setTimeout(() => (swallow.current = false), CLICK_GUARD_MS);
          navigator.vibrate?.(BUZZ_MS);
          toggle();
        }, LONG_PRESS_MS),
      };
    },
    onPointerMove: (event) => {
      const at = press.current;
      if (at && Math.hypot(event.clientX - at.x, event.clientY - at.y) > SLOP_PX) cancelPress();
    },
    onPointerUp: cancelPress,
    onPointerCancel: cancelPress,
    onClickCapture: (event) => {
      if (!swallow.current) return;
      swallow.current = false;
      event.preventDefault();
      event.stopPropagation();
    },
    'aria-haspopup': 'dialog',
    'aria-expanded': open,
    'data-quick': '',
  };
  return { open, setOpen, sub, setSub, anchor, setAnchor, props };
};
