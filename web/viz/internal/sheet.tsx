// SPDX-License-Identifier: Apache-2.0
import { type ReactNode, useEffect } from 'react';
import { createPortal } from 'react-dom';

/** Bottom sheet for the pinned summary on touch screens: closes on the backdrop and on Escape. */
export function VizMobileSheet({
  open,
  onClose,
  children,
}: {
  readonly open: boolean;
  readonly onClose: () => void;
  readonly children: ReactNode;
}) {
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => event.key === 'Escape' && onClose();
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);
  if (!open) return null;
  return createPortal(
    <div
      className="viz-root viz-msheet"
      onPointerDown={(event) => event.target === event.currentTarget && onClose()}
    >
      <div className="viz-msheet__panel" role="dialog" aria-modal="true">
        {children}
      </div>
    </div>,
    document.body,
  );
}
