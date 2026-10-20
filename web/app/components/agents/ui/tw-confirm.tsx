import {
  useId,
  useLayoutEffect,
  useRef,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type RefObject,
} from 'react';
import { createPortal } from 'react-dom';
import { cx, focusElement, focusFirst, STOP_REACT_BUBBLING, trapTab } from './dom';
import { BUTTON_VARIANT, TwButton } from './tw-button';
import { useDismissable, type TDismissReason } from './use-dismissable';
import { t } from '@/lib/i18n';

export const DIALOG_SIZE = {
  sm: 'sm',
  md: 'md',
  lg: 'lg',
} as const;

const SCROLL_LOCK_CLASS = 'tw-ui-scroll-lock';

let scrollLocks = 0;

const lockScroll = () => {
  scrollLocks += 1;
  document.documentElement.classList.add(SCROLL_LOCK_CLASS);
  return () => {
    scrollLocks -= 1;
    if (!scrollLocks) document.documentElement.classList.remove(SCROLL_LOCK_CLASS);
  };
};

/** Everything already on the page becomes inert; layers opened later from inside the dialog stay live. */
const makeBackgroundInert = (keep: readonly (HTMLElement | null)[]) => {
  const touched = [...document.body.children].filter(
    (element): element is HTMLElement =>
      element instanceof HTMLElement && !keep.includes(element) && !element.hasAttribute('inert'),
  );
  touched.forEach((element) => element.setAttribute('inert', ''));
  return () => touched.forEach((element) => element.removeAttribute('inert'));
};

export interface ITwDialogProps {
  open: boolean;
  onClose: (reason: TDismissReason) => void;
  title: string;
  description?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  role?: 'dialog' | 'alertdialog';
  size?: (typeof DIALOG_SIZE)[keyof typeof DIALOG_SIZE];
  /** Escape and a backdrop press close it. Turn off while an action that must finish is running. */
  dismissible?: boolean;
  initialFocusRef?: RefObject<HTMLElement | null>;
  className?: string;
}

/**
 * Modal dialog: the page behind is inert and does not scroll, Tab cycles inside, Escape and the
 * backdrop close it, focus returns to what opened it. On touch phones it is a bottom sheet (CSS).
 */
export const TwDialog = ({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  role = 'dialog',
  size = DIALOG_SIZE.sm,
  dismissible = true,
  initialFocusRef,
  className,
}: ITwDialogProps) => {
  const panelRef = useRef<HTMLDivElement>(null);
  const backdropRef = useRef<HTMLDivElement>(null);
  const titleId = useId();
  const descriptionId = useId();

  useDismissable(
    panelRef,
    (reason) => {
      if (dismissible) onClose(reason);
    },
    { enabled: open, modal: true, outside: false, scrimRef: backdropRef },
  );

  useLayoutEffect(() => {
    if (!open) return;
    const panel = panelRef.current;
    if (!panel) return;
    const restoreInert = makeBackgroundInert([panel, backdropRef.current]);
    const unlockScroll = lockScroll();
    if (initialFocusRef?.current) focusElement(initialFocusRef.current);
    else focusFirst(panel);
    return () => {
      restoreInert();
      unlockScroll();
    };
  }, [initialFocusRef, open]);

  if (!open || typeof document === 'undefined') return null;
  return createPortal(
    <>
      <div {...STOP_REACT_BUBBLING} ref={backdropRef} className="tw-ui-dialog-backdrop" aria-hidden="true" />
      <div
        {...STOP_REACT_BUBBLING}
        ref={panelRef}
        role={role}
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descriptionId : undefined}
        tabIndex={-1}
        data-size={size}
        className={cx('tw-ui-dialog', className)}
        onKeyDown={(event: ReactKeyboardEvent<HTMLDivElement>) => {
          event.stopPropagation();
          if (panelRef.current) trapTab(panelRef.current, event);
        }}
      >
        <h2 id={titleId} className="tw-ui-dialog-title">
          {title}
        </h2>
        {description && (
          <div id={descriptionId} className="tw-ui-dialog-description">
            {description}
          </div>
        )}
        {children && <div className="tw-ui-dialog-body">{children}</div>}
        {footer && <div className="tw-ui-dialog-footer">{footer}</div>}
      </div>
    </>,
    document.body,
  );
};

export const CONFIRM_TONE = {
  default: 'default',
  danger: 'danger',
} as const;

export interface ITwConfirmProps {
  open: boolean;
  title: string;
  description?: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  tone?: (typeof CONFIRM_TONE)[keyof typeof CONFIRM_TONE];
  /** The confirmed action is running: spinner, no second click, no dismissing. */
  pending?: boolean;
  /** Why the action failed; the dialog stays open so the user can retry or cancel. */
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
  children?: ReactNode;
}

/**
 * Confirmation before an action (role="alertdialog"). A dangerous action starts with focus on
 * "Cancel", so a reflexive Enter never deletes anything.
 */
export const TwConfirm = ({
  open,
  title,
  description,
  confirmLabel,
  cancelLabel = t('ui.twConfirm.cancel'),
  tone = CONFIRM_TONE.default,
  pending = false,
  error = null,
  onConfirm,
  onCancel,
  children,
}: ITwConfirmProps) => {
  const confirmRef = useRef<HTMLButtonElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const danger = tone === CONFIRM_TONE.danger;

  return (
    <TwDialog
      open={open}
      onClose={onCancel}
      title={title}
      description={description}
      role="alertdialog"
      dismissible={!pending}
      initialFocusRef={danger ? cancelRef : confirmRef}
      className="tw-ui-confirm"
      footer={
        <>
          <TwButton
            ref={cancelRef}
            variant={BUTTON_VARIANT.ghost}
            aria-disabled={pending || undefined}
            onClick={() => {
              if (!pending) onCancel();
            }}
          >
            {cancelLabel}
          </TwButton>
          <TwButton
            ref={confirmRef}
            variant={danger ? BUTTON_VARIANT.danger : BUTTON_VARIANT.primary}
            pending={pending}
            onClick={onConfirm}
          >
            {confirmLabel}
          </TwButton>
        </>
      }
    >
      {children || error ? (
        <>
          {children}
          {error && (
            <p role="alert" className="tw-ui-confirm-error">
              {error}
            </p>
          )}
        </>
      ) : null}
    </TwDialog>
  );
};
