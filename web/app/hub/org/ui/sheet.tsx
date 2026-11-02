// A sheet over the page: a panel from the right on a wide screen, from the bottom on a phone — one implementation.
// Closes on Escape, on the scrim and on the button, and plays its closing animation first.
import { type ReactNode, useEffect, useId, useRef } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';

import { IconButton } from '@kit';

import { useClosing } from './closing';

export interface SheetProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  sub?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}

export const Sheet = ({ open, onClose, title, sub, children, footer }: SheetProps) => {
  const { mounted, closing } = useClosing(open, 200);
  const titleId = useId();
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    panel.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);
  if (!mounted) return null;
  return createPortal(
    <div className="ho-sheet" data-closing={closing || undefined}>
      <div className="ho-sheet__scrim" onClick={onClose} />
      <div
        ref={panel}
        className="ho-sheet__panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
      >
        <header className="ho-sheet__head">
          <div>
            <h2 id={titleId} className="ho-sheet__title">
              {title}
            </h2>
            {sub != null && <p className="ho-sheet__sub">{sub}</p>}
          </div>
          <IconButton label="Закрыть" onClick={onClose}>
            <X size={16} />
          </IconButton>
        </header>
        <div className="ho-sheet__body">{children}</div>
        {footer != null && <footer className="ho-sheet__foot">{footer}</footer>}
      </div>
    </div>,
    document.body,
  );
};
