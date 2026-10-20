import {
  cloneElement,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { LAYER_ALIGN, LAYER_SIDE } from './placement';
import { TwLayerHostContext } from './tw-layer';
import { useAnchoredPosition } from './use-anchored-position';
import { openLayerCount } from './use-dismissable';

const SHOW_DELAY_MS = 450;
const HIDE_DELAY_MS = 120;

export const TOOLTIP_SIDE = {
  top: LAYER_SIDE.top,
  bottom: LAYER_SIDE.bottom,
  right: LAYER_SIDE.right,
  left: LAYER_SIDE.left,
} as const;

export interface ITwTooltipProps {
  /** Plain text: the trigger's description, and the bubble itself unless `bubble` says more. */
  content: string;
  /** A richer bubble for sighted users; `content` stays the description for everyone else. */
  bubble?: ReactNode;
  /** Wait before showing on hover, ms. Keyboard focus shows at once. */
  delay?: number;
  /** One focusable element (a button, a link). Its description points at the tooltip text. */
  children: ReactElement<{ 'aria-describedby'?: string }>;
  side?: (typeof TOOLTIP_SIDE)[keyof typeof TOOLTIP_SIDE];
}

/**
 * Supplementary hint on hover and keyboard focus. The text is always in the DOM as the trigger's
 * description, so screen readers and touch users never depend on the bubble. Escape hides it;
 * the pointer can move onto the bubble without it vanishing.
 */
export const TwTooltip = ({
  content,
  bubble,
  delay = SHOW_DELAY_MS,
  children,
  side = TOOLTIP_SIDE.top,
}: ITwTooltipProps) => {
  const descriptionId = useId();
  const host = useContext(TwLayerHostContext);
  const markerRef = useRef<HTMLSpanElement>(null);
  const anchorRef = useRef<HTMLElement | null>(null);
  const bubbleRef = useRef<HTMLDivElement>(null);
  const timer = useRef(0);
  const overBubble = useRef(false);
  const [open, setOpen] = useState(false);

  useAnchoredPosition(bubbleRef, anchorRef, {
    enabled: open,
    side,
    align: LAYER_ALIGN.center,
    offset: 8,
    margin: 8,
  });

  useLayoutEffect(() => {
    if (open) bubbleRef.current?.style.setProperty('--tw-ui-depth', String(openLayerCount()));
    else overBubble.current = false;
  }, [open]);

  useEffect(() => {
    const anchor = markerRef.current?.previousElementSibling;
    if (!(anchor instanceof HTMLElement)) return;
    anchorRef.current = anchor;
    const schedule = (next: boolean, delay: number) => {
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => {
        if (!next && overBubble.current) return;
        setOpen(next);
      }, delay);
    };
    const onPointerEnter = (event: PointerEvent) => {
      if (event.pointerType !== 'touch') schedule(true, delay);
    };
    const onPointerLeave = () => schedule(false, HIDE_DELAY_MS);
    const onFocus = () => {
      if (anchor.matches(':focus-visible')) schedule(true, 0);
    };
    const onBlur = () => schedule(false, 0);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      window.clearTimeout(timer.current);
      if (!bubbleRef.current) return;
      event.stopPropagation();
      setOpen(false);
    };
    anchor.addEventListener('pointerenter', onPointerEnter);
    anchor.addEventListener('pointerleave', onPointerLeave);
    anchor.addEventListener('focus', onFocus);
    anchor.addEventListener('blur', onBlur);
    anchor.addEventListener('keydown', onKeyDown);
    anchor.addEventListener('pointerdown', onBlur);
    return () => {
      window.clearTimeout(timer.current);
      anchor.removeEventListener('pointerenter', onPointerEnter);
      anchor.removeEventListener('pointerleave', onPointerLeave);
      anchor.removeEventListener('focus', onFocus);
      anchor.removeEventListener('blur', onBlur);
      anchor.removeEventListener('keydown', onKeyDown);
      anchor.removeEventListener('pointerdown', onBlur);
    };
  }, [delay]);

  const describedBy = [children.props['aria-describedby'], descriptionId].filter(Boolean).join(' ');

  return (
    <>
      {cloneElement(children, { 'aria-describedby': describedBy })}
      <span ref={markerRef} id={descriptionId} role="tooltip" className="tw-ui-visually-hidden">
        {content}
      </span>
      {open &&
        createPortal(
          <div
            ref={bubbleRef}
            aria-hidden="true"
            className="tw-ui-tooltip"
            onPointerEnter={() => {
              overBubble.current = true;
              window.clearTimeout(timer.current);
            }}
            onPointerLeave={() => {
              overBubble.current = false;
              setOpen(false);
            }}
          >
            {bubble ?? content}
          </div>,
          host ?? document.body,
        )}
    </>
  );
};
