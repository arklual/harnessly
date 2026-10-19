import { type ReactNode, useEffect, useId, useRef } from 'react';
import { X } from 'lucide-react';

import { IconButton } from './button';
import { cx } from './cx';

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: 's' | 'm' | 'l';
  className?: string;
}

/** A modal dialog on the native <dialog>: focus is trapped and Escape closes it; so does a press on the backdrop. */
export function Dialog({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  size = 'm',
  className,
}: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descId = useId();

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal?.();
    if (!open && d.open) d.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      className={cx('k-dialog', className)}
      data-size={size}
      aria-labelledby={titleId}
      aria-describedby={description != null ? descId : undefined}
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
    >
      {open && (
        <div className="k-dialog__panel">
          <header className="k-dialog__header">
            <div>
              <h2 id={titleId} className="k-dialog__title">
                {title}
              </h2>
              {description != null && (
                <p id={descId} className="k-dialog__description">
                  {description}
                </p>
              )}
            </div>
            <IconButton label="Close" size="xs" onClick={onClose}>
              <X size={16} />
            </IconButton>
          </header>
          {children != null && <div className="k-dialog__body">{children}</div>}
          {footer != null && <footer className="k-dialog__footer">{footer}</footer>}
        </div>
      )}
    </dialog>
  );
}
