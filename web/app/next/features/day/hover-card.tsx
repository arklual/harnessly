// A card that opens from a thing on the calendar strip, on the shared hover intent (ui/use-hover-intent.ts): the pointer
// may travel to it without it closing, a tap toggles it on touch, Esc and a press outside close it, a card opened from
// inside another keeps its parent open. It mounts inside .hx-root and leaves with an animation.
import {
  cloneElement,
  useLayoutEffect,
  useRef,
  type FocusEvent,
  type PointerEvent,
  type ReactElement,
  type ReactNode,
  type Ref,
} from 'react';
import { createPortal } from 'react-dom';
import { placeFloating, rectOf, SIDE, type TSide } from '@/next/ui/hover-intent';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresentFlag } from '@/next/ui/presence';
import { HoverParentContext, useHoverIntent } from '@/next/ui/use-hover-intent';

const EXIT_MS = 140;
const GAP = 6;
const PAD = 8;

export const CARD_PLACE = { under: 'under', over: 'over', beside: 'beside' } as const;
export type TCardPlace = (typeof CARD_PLACE)[keyof typeof CARD_PLACE];

type THandler<E> = ((event: E) => void) | undefined;

interface IChildProps {
  ref?: Ref<HTMLElement>;
  onPointerEnter?: THandler<PointerEvent<HTMLElement>>;
  onPointerLeave?: THandler<PointerEvent<HTMLElement>>;
  onFocus?: THandler<FocusEvent<HTMLElement>>;
  onBlur?: THandler<FocusEvent<HTMLElement>>;
  'aria-expanded'?: boolean;
}

interface IHoverCardProps {
  className?: string;
  /** Under the thing by default; a stack opens its list over it, a row of a list its details beside the list. */
  place?: TCardPlace;
  /** Nothing to say: the thing stays as it is. */
  disabled?: boolean;
  /** What the card shows; it gets a function that closes the card (a row that opens a thing closes it first). */
  card: (close: () => void) => ReactNode;
  /** The thing; `toggle` is for its own click when the click has nothing else to do (a stack, a tap on touch). */
  children: (toggle: () => void) => ReactElement;
}

const both =
  <E,>(own: THandler<E>, ours: (event: E) => void) =>
  (event: E) => {
    own?.(event);
    ours(event);
  };

const assign = <T,>(ref: Ref<T> | undefined, value: T | null) => {
  if (typeof ref === 'function') ref(value);
  else if (ref) ref.current = value;
};

const clamp = (value: number, min: number, max: number) => Math.max(min, Math.min(value, Math.max(min, max)));

const HoverCard = ({
  className,
  place = CARD_PLACE.under,
  disabled = false,
  card,
  children,
}: IHoverCardProps) => {
  const container = usePortalContainer();
  const { open, getTrigger, setTrigger, setFloating, triggerProps, floatingProps, close, parent } =
    useHoverIntent({ disabled });
  const { mounted, state } = usePresentFlag(open && !disabled, EXIT_MS);
  const posRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const pos = posRef.current;
    const anchor = getTrigger();
    if (!pos || !anchor || !open) return;
    const size = { width: pos.offsetWidth, height: pos.offsetHeight };
    const viewport = { width: window.innerWidth, height: window.innerHeight };
    const rect = rectOf(anchor);
    let x: number;
    let y: number;
    let side: string;
    if (place === CARD_PLACE.beside) {
      // Beside the list the row is in; on the other side when there is no room.
      const list = anchor.closest<HTMLElement>('.hx-day-pop');
      const around = list ? rectOf(list) : rect;
      const right = around.right + GAP + size.width <= viewport.width - PAD;
      x = right ? around.right + GAP : around.left - GAP - size.width;
      y = clamp(rect.bottom - size.height, PAD, viewport.height - size.height - PAD);
      side = right ? 'right' : 'left';
    } else {
      const want: TSide = place === CARD_PLACE.over ? SIDE.top : SIDE.bottom;
      const placed = placeFloating({ anchor: rect, size, viewport, side: want, align: 'start', offset: GAP });
      ({ x, y, side } = placed);
    }
    pos.style.setProperty('--x', `${Math.round(x)}px`);
    pos.style.setProperty('--y', `${Math.round(y)}px`);
    pos.dataset.side = side;
  }, [open, mounted, getTrigger, place]);

  const child = children(triggerProps.onClick);
  const own = child.props as IChildProps;
  const trigger = cloneElement(child as ReactElement<IChildProps>, {
    ref: (el: HTMLElement | null) => {
      setTrigger(el);
      assign(own.ref, el);
    },
    onPointerEnter: both(own.onPointerEnter, triggerProps.onPointerEnter),
    onPointerLeave: both(own.onPointerLeave, triggerProps.onPointerLeave),
    onFocus: both(own.onFocus, triggerProps.onFocus),
    onBlur: both(own.onBlur, triggerProps.onBlur),
    'aria-expanded': mounted ? true : undefined,
  });

  const host = container?.current;
  return (
    <>
      {trigger}
      {mounted &&
        host &&
        createPortal(
          <div
            ref={(el) => {
              posRef.current = el;
              setFloating(el);
            }}
            className={className ? `hx-day-pop ${className}` : 'hx-day-pop'}
            data-state={state}
            {...floatingProps}
          >
            <HoverParentContext value={parent}>{card(close)}</HoverParentContext>
          </div>,
          host,
        )}
    </>
  );
};

export default HoverCard;
