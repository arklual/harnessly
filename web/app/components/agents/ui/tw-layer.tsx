import {
  createContext,
  useContext,
  useRef,
  type HTMLAttributes,
  type ReactNode,
  type RefObject,
} from 'react';
import { createPortal } from 'react-dom';
import { cx, STOP_REACT_BUBBLING } from './dom';
import { LAYER_ALIGN, LAYER_SIDE, type TLayerAlign, type TLayerSide } from './placement';
import { useAnchoredPosition, type TTwAnchor } from './use-anchored-position';
import { useDismissable, type TDismissReason } from './use-dismissable';

/** Where floating layers are portalled when not document.body (a host rendered in a shadow root or a frame). */
export const TwLayerHostContext = createContext<HTMLElement | null>(null);

export const LAYER_KIND = {
  popover: 'popover',
  menu: 'menu',
  listbox: 'listbox',
} as const;

export interface ITwLayerProps extends Omit<HTMLAttributes<HTMLDivElement>, 'children'> {
  open: boolean;
  anchor: TTwAnchor;
  onDismiss: (reason: TDismissReason) => void;
  kind: (typeof LAYER_KIND)[keyof typeof LAYER_KIND];
  children: ReactNode;
  side?: TLayerSide;
  align?: TLayerAlign;
  offset?: number;
  layerRef?: RefObject<HTMLDivElement | null>;
  triggerRef?: RefObject<HTMLElement | null>;
  /** On touch phones the layer becomes a bottom sheet with a scrim (CSS only). Default true. */
  sheet?: boolean;
  /** At least as wide as the anchor (select-like popups). */
  matchAnchorWidth?: boolean;
  escape?: boolean;
  focusOutside?: boolean;
  restoreFocus?: boolean;
}

const LAYER_MARGIN = 8;

/**
 * Portalled, anchored surface shared by popovers, menus and listbox popups. React events inside it
 * do not bubble to the trigger's ancestors, so a popover inside a clickable row or a composer never
 * fires the row's click or the composer's Enter.
 */
export const TwLayer = (props: ITwLayerProps) =>
  props.open && typeof document !== 'undefined' ? <OpenLayer {...props} /> : null;

/**
 * The layer while it is open. A closed layer renders nothing and runs no hooks: a screen keeps hundreds of closed
 * menus and popovers mounted, and every one of them used to set up its listeners on each render.
 */
const OpenLayer = ({
  open,
  anchor,
  onDismiss,
  kind,
  children,
  side = LAYER_SIDE.bottom,
  align = LAYER_ALIGN.start,
  offset = 6,
  layerRef,
  triggerRef,
  sheet = true,
  matchAnchorWidth = false,
  escape = true,
  focusOutside = true,
  restoreFocus = true,
  className,
  onKeyDown,
  onClick,
  onPointerDown,
  onContextMenu,
  ...rest
}: ITwLayerProps) => {
  const host = useContext(TwLayerHostContext);
  const ownRef = useRef<HTMLDivElement>(null);
  const ref = layerRef ?? ownRef;
  const scrimRef = useRef<HTMLDivElement>(null);

  useDismissable(ref, onDismiss, { enabled: open, triggerRef, escape, focusOutside, restoreFocus, scrimRef });
  useAnchoredPosition(ref, anchor, { enabled: open, side, align, offset, margin: LAYER_MARGIN });

  return createPortal(
    <>
      {sheet && <div {...STOP_REACT_BUBBLING} ref={scrimRef} className="tw-ui-scrim" aria-hidden="true" />}
      <div
        {...rest}
        ref={ref}
        className={cx('tw-ui-layer', `tw-ui-layer-${kind}`, className)}
        data-sheet={sheet ? '' : undefined}
        data-match-anchor={matchAnchorWidth ? '' : undefined}
        onKeyDown={(event) => {
          onKeyDown?.(event);
          event.stopPropagation();
        }}
        onClick={(event) => {
          onClick?.(event);
          event.stopPropagation();
        }}
        onPointerDown={(event) => {
          onPointerDown?.(event);
          event.stopPropagation();
        }}
        onContextMenu={(event) => {
          onContextMenu?.(event);
          event.stopPropagation();
        }}
      >
        {children}
      </div>
    </>,
    host ?? document.body,
  );
};
