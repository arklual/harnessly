// The bottom sheet of the phone (mobile.css of the design), one primitive for every panel that is a popover on a wide
// window: it rises in 420 ms over a scrim, leaves in 280 ms, sits above the safe area, is as tall as its content up to
// 90% of the screen and scrolls inside. It closes on the scrim, on Esc, and on a swipe down: past a third of its height,
// or a quick flick whatever the distance. A swipe that starts in a scrolled list scrolls the list instead. Focus stays
// inside while it is open and goes back where it was when it closes.
//
// Sheet draws all of it. A popover that keeps its own markup on a wide window turns into a sheet on the phone with the
// parts: SheetScrim under it, SheetGrip at its top, useSheetDrag and useFocusTrap on its element and the class
// `hx-sheet-phone`, which gives it the sheet's place and look only below the phone breakpoint (styles/ui.css).
import { useEffect, useEffectEvent, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresentFlag, type TPresenceState } from '@/next/ui/presence';

export const SHEET_EXIT_MS = 280;
/** A flick down faster than this closes the sheet whatever the distance, px/ms. */
const CLOSE_VELOCITY = 0.5;
/** A slow drag closes it past this share of its height. */
const CLOSE_SHARE = 1 / 3;
/** How far the finger goes before the move counts as a drag (or as a scroll sideways), px. */
const SLOP_PX = 6;
/** A flick shorter than this is a tap that wobbled, px. */
const MIN_FLICK_PX = 24;

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** An element between the touch and the sheet that is scrolled down: the finger scrolls it, it does not move the sheet. */
const scrolledBetween = (from: EventTarget | null, sheet: HTMLElement): boolean => {
  for (
    let node = from instanceof Element ? from : null;
    node && node !== sheet.parentElement;
    node = node.parentElement
  ) {
    if (node.scrollTop > 0 && node.scrollHeight > node.clientHeight) return true;
  }
  return false;
};

/**
 * Follows a finger dragging the sheet down: the offset goes straight into `--hx-sheet-dy` (no state per frame) and
 * `data-drag` stops the transition; on release it closes or springs back. Touch only: a mouse never drags a sheet.
 */
export const useSheetDrag = (
  target: HTMLElement | null | (() => HTMLElement | null),
  onClose: () => void,
  enabled = true,
) => {
  const close = useEffectEvent(onClose);
  useEffect(() => {
    const sheet = typeof target === 'function' ? target() : target;
    if (!sheet || !enabled) return;
    let start: { x: number; y: number } | null = null;
    let dragging = false;
    let dy = 0;
    let velocity = 0;
    let last = { y: 0, t: 0 };

    const reset = () => {
      start = null;
      dragging = false;
      delete sheet.dataset.drag;
      sheet.style.removeProperty('--hx-sheet-dy');
    };
    const down = (event: TouchEvent) => {
      if (event.touches.length !== 1 || scrolledBetween(event.target, sheet)) return;
      const touch = event.touches[0];
      start = { x: touch.clientX, y: touch.clientY };
      dragging = false;
      dy = 0;
      velocity = 0;
      last = { y: touch.clientY, t: event.timeStamp };
    };
    const move = (event: TouchEvent) => {
      if (!start) return;
      const touch = event.touches[0];
      const dx = touch.clientX - start.x;
      const down = touch.clientY - start.y;
      if (!dragging) {
        // Up, or sideways (a slider, a strip of chips): not ours.
        if (down < -SLOP_PX || Math.abs(dx) > SLOP_PX + Math.max(0, down)) {
          start = null;
          return;
        }
        if (down < SLOP_PX) return;
        dragging = true;
        sheet.dataset.drag = 'true';
      }
      if (event.cancelable) event.preventDefault();
      const dt = event.timeStamp - last.t;
      if (dt > 0) velocity = (touch.clientY - last.y) / dt;
      last = { y: touch.clientY, t: event.timeStamp };
      dy = Math.max(0, down - SLOP_PX);
      sheet.style.setProperty('--hx-sheet-dy', `${dy}px`);
    };
    const up = () => {
      if (!start) return;
      const was = dragging;
      reset();
      if (!was) return;
      if (dy > sheet.offsetHeight * CLOSE_SHARE || (velocity > CLOSE_VELOCITY && dy > MIN_FLICK_PX)) close();
    };
    sheet.addEventListener('touchstart', down, { passive: true });
    sheet.addEventListener('touchmove', move, { passive: false });
    sheet.addEventListener('touchend', up);
    sheet.addEventListener('touchcancel', up);
    return () => {
      reset();
      sheet.removeEventListener('touchstart', down);
      sheet.removeEventListener('touchmove', move);
      sheet.removeEventListener('touchend', up);
      sheet.removeEventListener('touchcancel', up);
    };
  }, [target, enabled]);
};

/** Tab and Shift+Tab go round inside the element while it is open; the focus goes back where it was when it closes. */
export const useFocusTrap = (box: HTMLElement | null, enabled = true) => {
  useEffect(() => {
    if (!box || !enabled) return;
    const before = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (!box.contains(document.activeElement)) box.focus({ preventScroll: true });
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Tab') return;
      const all = [...box.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.offsetParent !== null);
      if (all.length === 0) {
        event.preventDefault();
        return;
      }
      const first = all[0];
      const last = all[all.length - 1];
      if (event.shiftKey && (document.activeElement === first || document.activeElement === box)) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    box.addEventListener('keydown', onKey);
    return () => {
      box.removeEventListener('keydown', onKey);
      if (before?.isConnected) before.focus({ preventScroll: true });
    };
  }, [box, enabled]);
};

interface ISheetScrimProps {
  state: TPresenceState;
  onClose: () => void;
  /** Under a popover that is a sheet only on the phone: no scrim on a wide window. */
  phoneOnly?: boolean;
}

/** The dimmed window under a sheet; a tap on it closes the sheet. */
export const SheetScrim = ({ state, onClose, phoneOnly = false }: ISheetScrimProps) => (
  <div
    className="hx-sheet-scrim"
    data-state={state}
    data-phone={phoneOnly || undefined}
    aria-hidden="true"
    onClick={onClose}
  />
);

/** The handle at the top of a sheet: it says the sheet can be pulled down. Hidden on a wide window. */
export const SheetGrip = () => <span className="hx-sheet__grip" aria-hidden="true" />;

interface ISheetProps {
  open: boolean;
  onClose: () => void;
  label: string;
  className?: string;
  children: ReactNode;
}

const Sheet = ({ open, onClose, label, className, children }: ISheetProps) => {
  const { mounted, state } = usePresentFlag(open, SHEET_EXIT_MS);
  const container = usePortalContainer();
  const [sheet, setSheet] = useState<HTMLDivElement | null>(null);
  const close = useEffectEvent(onClose);

  useEffect(() => {
    if (!open || !sheet) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      // A sheet opened from a sheet (a menu in a dialog) closes alone: Esc is for the one on top.
      const shown = [...document.querySelectorAll('.hx-sheet:not([data-state="closing"])')];
      if (shown[shown.length - 1] === sheet) close();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, sheet]);
  useSheetDrag(sheet, onClose, open);
  useFocusTrap(sheet, open);

  if (!mounted) return null;
  const body = (
    <>
      <SheetScrim state={state} onClose={onClose} />
      <div
        ref={setSheet}
        className={className ? `hx-sheet ${className}` : 'hx-sheet'}
        data-state={state}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
      >
        <SheetGrip />
        <div className="hx-sheet__body">{children}</div>
      </div>
    </>
  );
  return container?.current ? createPortal(body, container.current) : body;
};

export default Sheet;
