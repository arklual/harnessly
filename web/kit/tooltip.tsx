import { type ReactNode, useCallback, useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

import { cx } from './cx';
import { place, type Side } from './position';

export interface TooltipProps {
  content: ReactNode;
  children: ReactNode;
  side?: Side;
  /** Milliseconds of hover before it shows (focus shows it at once). */
  delay?: number;
  className?: string;
}

/** A short label shown next to its child on hover or keyboard focus. */
export function Tooltip({ content, children, side = 'top', delay = 450, className }: TooltipProps) {
  const anchor = useRef<HTMLSpanElement>(null);
  const bubble = useRef<HTMLDivElement>(null);
  const timer = useRef<number | undefined>(undefined);
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number; side: Side } | null>(null);
  const id = useId();

  const show = useCallback(
    (now: boolean) => {
      window.clearTimeout(timer.current);
      if (now) setOpen(true);
      else timer.current = window.setTimeout(() => setOpen(true), delay);
    },
    [delay],
  );
  const hide = useCallback(() => {
    window.clearTimeout(timer.current);
    setOpen(false);
    setPos(null);
  }, []);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  useLayoutEffect(() => {
    if (!open || !anchor.current || !bubble.current) return;
    const r = anchor.current.getBoundingClientRect();
    const p = place(
      { left: r.left, top: r.top, width: r.width, height: r.height },
      { width: bubble.current.offsetWidth, height: bubble.current.offsetHeight },
      { width: window.innerWidth, height: window.innerHeight },
      { side, align: 'center' },
    );
    setPos({ left: p.left, top: p.top, side: p.side });
  }, [open, side]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && hide();
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, hide]);

  return (
    <span
      ref={anchor}
      className="k-tooltip-anchor"
      aria-describedby={open ? id : undefined}
      onPointerEnter={(e) => e.pointerType === 'mouse' && show(false)}
      onPointerLeave={hide}
      onFocus={(e) => e.target.matches(':focus-visible') && show(true)}
      onBlur={hide}
    >
      {children}
      {open &&
        createPortal(
          <div
            ref={bubble}
            id={id}
            role="tooltip"
            className={cx('k-tooltip', className)}
            data-side={pos?.side ?? side}
            style={pos ? { left: pos.left, top: pos.top } : { visibility: 'hidden', left: 0, top: 0 }}
          >
            {content}
          </div>,
          document.body,
        )}
    </span>
  );
}
