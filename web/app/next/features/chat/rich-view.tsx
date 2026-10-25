// A diagram or an HTML answer full size over a scrim: it grows in and shrinks out like a picture does; Esc, the scrim
// and the cross close it. What it shows is the caller's: the diagram's drawing, or the page's frame.
import { useEffect, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';

export const RICH_VIEW_EXIT_MS = 200;

interface IRichViewProps {
  open: boolean;
  title: string;
  /** What the body is: a diagram scrolls and scales, a page fills the panel. */
  kind: 'diagram' | 'page';
  onClose: () => void;
  children: ReactNode;
}

const RichView = ({ open, title, kind, onClose, children }: IRichViewProps) => {
  const { mounted, state } = usePresentFlag(open, RICH_VIEW_EXIT_MS);
  const container = usePortalContainer();

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!mounted) return null;
  const body = (
    <div
      className="hx-richview"
      data-state={state}
      role="dialog"
      aria-modal="true"
      aria-label={title}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="hx-richview__panel" data-kind={kind}>
        <div className="hx-richview__head">
          <span className="hx-richview__title">{title}</span>
          <button type="button" className="hx-rich__btn" aria-label={t('next.rich.close')} onClick={onClose}>
            <Icon name="x" size="sm" />
          </button>
        </div>
        <div className="hx-richview__body" data-kind={kind}>
          {children}
        </div>
      </div>
    </div>
  );
  return container?.current ? createPortal(body, container.current) : body;
};

export default RichView;
