import { type CSSProperties, type ReactNode, type RefObject, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import { cx } from './cx';
import { place, type Placement } from './position';

export interface PopupProps {
  anchor: RefObject<HTMLElement | null>;
  open: boolean;
  onClose: () => void;
  placement?: Placement;
  className?: string;
  children: ReactNode;
  role?: string;
  /** Closes on a press outside the popup and its anchor (default true). */
  dismissOnOutside?: boolean;
  matchWidth?: boolean;
}

/** A floating panel attached to an anchor, rendered in a portal; closes on Escape and outside presses. */
export function Popup({
  anchor,
  open,
  onClose,
  placement = { side: 'bottom', align: 'start' },
  className,
  children,
  role,
  dismissOnOutside = true,
  matchWidth,
}: PopupProps) {
  const ref = useRef<HTMLDivElement>(null);
  const [style, setStyle] = useState<CSSProperties>({ visibility: 'hidden' });

  useLayoutEffect(() => {
    if (!open) return;
    const update = () => {
      const a = anchor.current;
      const p = ref.current;
      if (!a || !p) return;
      const r = a.getBoundingClientRect();
      const placed = place(
        { left: r.left, top: r.top, width: r.width, height: r.height },
        { width: p.offsetWidth, height: p.scrollHeight },
        { width: window.innerWidth, height: window.innerHeight },
        placement,
      );
      setStyle({
        left: placed.left,
        top: placed.top,
        maxHeight: placed.maxHeight,
        minWidth: matchWidth ? r.width : undefined,
      });
    };
    update();
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, true);
    return () => {
      window.removeEventListener('resize', update);
      window.removeEventListener('scroll', update, true);
    };
  }, [open, anchor, placement, matchWidth]);

  useLayoutEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        onClose();
        anchor.current?.focus();
      }
    };
    const onDown = (e: PointerEvent) => {
      const t = e.target as Node;
      if (ref.current?.contains(t) || anchor.current?.contains(t)) return;
      onClose();
    };
    document.addEventListener('keydown', onKey, true);
    if (dismissOnOutside) document.addEventListener('pointerdown', onDown, true);
    return () => {
      document.removeEventListener('keydown', onKey, true);
      document.removeEventListener('pointerdown', onDown, true);
    };
  }, [open, onClose, anchor, dismissOnOutside]);

  if (!open) return null;
  return createPortal(
    <div ref={ref} className={cx('k-popup', className)} style={style} role={role}>
      {children}
    </div>,
    document.body,
  );
}
