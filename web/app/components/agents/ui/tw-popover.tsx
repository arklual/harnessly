import { useId, useLayoutEffect, useRef, type ReactNode, type RefObject } from 'react';
import { cx, focusElement, focusFirst } from './dom';
import { LAYER_ALIGN, LAYER_SIDE, type TLayerAlign } from './placement';
import { LAYER_KIND, TwLayer } from './tw-layer';
import type { TDismissReason } from './use-dismissable';

export const POPOVER_SIDE = {
  top: LAYER_SIDE.top,
  bottom: LAYER_SIDE.bottom,
} as const;

export interface ITwPopoverProps {
  open: boolean;
  onClose: (reason: TDismissReason) => void;
  anchorRef: RefObject<HTMLElement | null>;
  children: ReactNode;
  /** Opens below by default; flips when there is no room. */
  side?: (typeof POPOVER_SIDE)[keyof typeof POPOVER_SIDE];
  align?: TLayerAlign;
  /** Visible heading; also names the dialog. */
  title?: string;
  /** Accessible name when there is no visible title. */
  'aria-label'?: string;
  /** Focused on open. Otherwise the first focusable element, else the popover itself. */
  initialFocusRef?: RefObject<HTMLElement | null>;
  id?: string;
  className?: string;
  /** Bottom sheet on touch phones. Default true. */
  sheet?: boolean;
}

/**
 * Non-modal dialog anchored to a trigger: role="dialog", Escape and an outside press close it,
 * focus moves in on open and back to the trigger on close. The trigger owns its own state and
 * spreads twPopoverTriggerProps() for aria-expanded / aria-controls.
 */
export const TwPopover = (props: ITwPopoverProps) => (props.open ? <OpenPopover {...props} /> : null);

/** The popover while it is open; closed, it renders nothing and runs no hooks (see TwLayer). */
const OpenPopover = ({
  open,
  onClose,
  anchorRef,
  children,
  side = POPOVER_SIDE.bottom,
  align = LAYER_ALIGN.start,
  title,
  'aria-label': ariaLabel,
  initialFocusRef,
  id,
  className,
  sheet = true,
}: ITwPopoverProps) => {
  const layerRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useLayoutEffect(() => {
    if (!open) return;
    const layer = layerRef.current;
    if (!layer) return;
    if (initialFocusRef?.current) focusElement(initialFocusRef.current);
    else focusFirst(layer);
  }, [initialFocusRef, open]);

  return (
    <TwLayer
      open={open}
      anchor={anchorRef}
      triggerRef={anchorRef}
      onDismiss={onClose}
      kind={LAYER_KIND.popover}
      side={side}
      align={align}
      layerRef={layerRef}
      sheet={sheet}
      id={id}
      role="dialog"
      aria-modal="false"
      aria-label={title ? undefined : ariaLabel}
      aria-labelledby={title ? titleId : undefined}
      tabIndex={-1}
      className={cx('tw-ui-popover', className)}
    >
      {title && (
        <h2 id={titleId} className="tw-ui-popover-title">
          {title}
        </h2>
      )}
      {children}
    </TwLayer>
  );
};

/** ARIA wiring for the element that opens a popover or menu. */
export const twPopoverTriggerProps = (
  open: boolean,
  controls: string,
  haspopup: 'dialog' | 'menu' | 'listbox' = 'dialog',
) =>
  ({
    'aria-haspopup': haspopup,
    'aria-expanded': open,
    'aria-controls': open ? controls : undefined,
  }) as const;
