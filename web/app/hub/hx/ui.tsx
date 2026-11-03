// The new hub's building blocks, drawn with the design's own classes: an icon, a button, a popover, a drawer and a
// dialog. Each one opens with the design's ease-out animation and closes through data-closing first (popover 140 ms,
// dialog 160 ms, drawer 200 ms), so nothing vanishes in one frame.
import {
  type ButtonHTMLAttributes,
  createContext,
  type ReactNode,
  type RefObject,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from 'react';
import { createPortal } from 'react-dom';

import { useClosing } from '../org/ui/closing';

/** The layer at the root of the hub where drawers and dialogs draw, above the scrolling page (as on the board). */
export const LayerCtx = createContext<HTMLElement | null>(null);

const InLayer = ({ children }: { children: ReactNode }) => {
  const layer = useContext(LayerCtx);
  return layer ? createPortal(children, layer) : <>{children}</>;
};

export const POP_MS = 140;
export const DIALOG_MS = 160;
export const DRAWER_MS = 200;

export const Icon = ({
  i,
  size,
  className,
}: {
  i: string;
  size?: 'xs' | 'sm' | 'lg';
  className?: string;
}) => (
  <i
    className={className ? `hx-icon ${className}` : 'hx-icon'}
    data-i={i}
    data-size={size}
    aria-hidden="true"
  />
);

export interface BtnProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'ghost' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  shape?: 'icon' | 'pill';
  icon?: string;
  busy?: boolean;
}

/** `.hx-btn` with its data attributes; `busy` keeps the width and shows the design's spinner. */
export const Btn = ({
  variant,
  size,
  shape,
  icon,
  busy,
  children,
  className,
  type,
  disabled,
  ...rest
}: BtnProps) => (
  <button
    type={type ?? 'button'}
    className={className ? `hx-btn ${className}` : 'hx-btn'}
    data-variant={variant}
    data-size={size}
    data-shape={shape}
    data-busy={busy ? 'true' : undefined}
    disabled={disabled || busy}
    {...rest}
  >
    {icon && <Icon i={icon} size={size === 'sm' ? 'sm' : undefined} />}
    {children}
  </button>
);

/** `data-closing` the way the board's CSS reads it: "1" while closing, absent otherwise. */
export const closingAttr = (closing: boolean): '1' | undefined => (closing ? '1' : undefined);

/**
 * A popover's state: open, close with its animation, close on Escape and on a click outside `root`.
 * The popover itself is markup of the screen (each one has its own class in the design).
 */
export const usePop = (root: RefObject<HTMLElement | null>, ms = POP_MS) => {
  const [open, setOpen] = useState(false);
  const { mounted, closing } = useClosing(open, ms);
  const close = useCallback(() => setOpen(false), []);
  const toggle = useCallback(() => setOpen((v) => !v), []);
  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('pointerdown', onDown, true);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('pointerdown', onDown, true);
      document.removeEventListener('keydown', onKey);
    };
  }, [open, root]);
  return { open, mounted, closing, setOpen, close, toggle, attr: closingAttr(closing) };
};

const useEscape = (on: boolean, fn: () => void) => {
  const ref = useRef(fn);
  useEffect(() => {
    ref.current = fn;
  });
  useEffect(() => {
    if (!on) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') ref.current();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [on]);
};

export interface DrawerProps {
  open: boolean;
  onClose: () => void;
  /** Extra classes of the panel: `md`, `md ps`, `md es`… */
  className?: string;
  label: string;
  /** The head: an icon or avatar before the title. */
  lead?: ReactNode;
  title: ReactNode;
  sub?: ReactNode;
  /** Buttons in the head before the close button. */
  actions?: ReactNode;
  children: ReactNode;
  /** Something drawn over the drawer (its dialog). */
  over?: ReactNode;
}

/** The right-side sheet of the board (`.ad-scrim` + `aside.ad-sheet`), a bottom sheet on a phone. */
export const Drawer = ({
  open,
  onClose,
  className,
  label,
  lead,
  title,
  sub,
  actions,
  children,
  over,
}: DrawerProps) => {
  const { mounted, closing } = useClosing(open, DRAWER_MS);
  const panel = useRef<HTMLElement>(null);
  useEscape(open, onClose);
  useEffect(() => {
    if (open) panel.current?.focus({ preventScroll: true });
  }, [open]);
  if (!mounted) return null;
  const c = closingAttr(closing);
  return (
    <InLayer>
      <div className="ad-scrim" data-closing={c} onClick={onClose} />
      <aside
        ref={panel}
        className={className ? `ad-sheet ${className}` : 'ad-sheet'}
        data-closing={c}
        aria-label={label}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
      >
        <div className="ad-sheet__head">
          {lead}
          <h3>
            {title}
            {sub != null && <small>{sub}</small>}
          </h3>
          {actions}
          <Btn variant="ghost" size="sm" shape="icon" onClick={onClose} aria-label="Закрыть">
            <Icon i="x" />
          </Btn>
        </div>
        <div className="ad-sheet__body">{children}</div>
        {over}
      </aside>
    </InLayer>
  );
};

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  label: string;
  /** The whole inside: `md-dlg__ic`, `h4`, the body and `md-dlg__foot`, as each dialog of the board draws it. */
  children: ReactNode;
  /** Draws in place instead of in the hub layer: a dialog of a drawer sits inside the drawer, over its content. */
  inline?: boolean;
}

/** A modal dialog of the board (`md-dlg-scrim` + `md-dlg`) with its 160 ms close. */
export const Dialog = ({ open, onClose, label, children, inline }: DialogProps) => {
  const { mounted, closing } = useClosing(open, DIALOG_MS);
  useEscape(open, onClose);
  if (!mounted) return null;
  const c = closingAttr(closing);
  const body = (
    <>
      <div className="md-dlg-scrim" data-closing={c} onClick={onClose} />
      <div className="md-dlg" data-closing={c} role="dialog" aria-modal="true" aria-label={label}>
        {children}
      </div>
    </>
  );
  return inline ? body : <InLayer>{body}</InLayer>;
};

/** The first letter of a name for an avatar or a mark. */
export const initial = (name: string): string => name.trim().slice(0, 1).toUpperCase() || '·';
