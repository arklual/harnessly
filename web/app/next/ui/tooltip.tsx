// A hint over a control, on the shared hover intent (use-hover-intent.ts), no library: it waits 450 ms like the CSS hints
// (.hx-tipped), opens at once when another hint was just open (moving along a row of buttons), never shows on touch,
// mounts inside .hx-root and leaves with an animation. It prefers the top and flips below when there is no room: it
// never covers the control it explains.
import {
  cloneElement,
  useId,
  useLayoutEffect,
  useRef,
  type FocusEvent,
  type PointerEvent,
  type ReactElement,
  type Ref,
} from 'react';
import { createPortal } from 'react-dom';
import { HOVER, placeFloating, rectOf, SIDE, TIP_WARMTH, type TSide } from '@/next/ui/hover-intent';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresentFlag } from '@/next/ui/presence';
import { useHoverIntent } from '@/next/ui/use-hover-intent';

const EXIT_MS = 140;

interface ITooltipProps {
  label: string;
  keys?: string[];
  /** The preferred side; the hint flips when there is no room there. */
  side?: TSide;
  /** No hint now (a menu is open over the control, a drag is on): an open one leaves. */
  disabled?: boolean;
  children: ReactElement;
}

type THandler<E> = ((event: E) => void) | undefined;

interface IChildProps {
  ref?: Ref<HTMLElement>;
  onPointerEnter?: THandler<PointerEvent<HTMLElement>>;
  onPointerLeave?: THandler<PointerEvent<HTMLElement>>;
  onFocus?: THandler<FocusEvent<HTMLElement>>;
  onBlur?: THandler<FocusEvent<HTMLElement>>;
  onClick?: THandler<unknown>;
  'aria-describedby'?: string;
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

const Tooltip = ({ label, keys, side = SIDE.top, disabled = false, children }: ITooltipProps) => {
  const container = usePortalContainer();
  const { open, getTrigger, setTrigger, triggerProps } = useHoverIntent({
    openDelay: HOVER.tipDelay,
    interactive: false,
    warmth: TIP_WARMTH,
  });
  const { mounted, state } = usePresentFlag(open && !disabled, EXIT_MS);
  const posRef = useRef<HTMLDivElement>(null);
  const tipRef = useRef<HTMLDivElement>(null);
  const id = useId();

  useLayoutEffect(() => {
    const pos = posRef.current;
    const tip = tipRef.current;
    const anchor = getTrigger();
    if (!pos || !tip || !anchor || !open) return;
    const place = placeFloating({
      anchor: rectOf(anchor),
      size: { width: tip.offsetWidth, height: tip.offsetHeight },
      viewport: { width: window.innerWidth, height: window.innerHeight },
      side,
    });
    pos.style.setProperty('--x', `${Math.round(place.x)}px`);
    pos.style.setProperty('--y', `${Math.round(place.y)}px`);
    tip.dataset.side = place.side;
  }, [open, getTrigger, mounted, side, label]);

  const own = children.props as IChildProps;
  const trigger = cloneElement(children as ReactElement<IChildProps>, {
    ref: (el: HTMLElement | null) => {
      setTrigger(el);
      assign(own.ref, el);
    },
    onPointerEnter: both(own.onPointerEnter, triggerProps.onPointerEnter),
    onPointerLeave: both(own.onPointerLeave, triggerProps.onPointerLeave),
    onFocus: both(own.onFocus, triggerProps.onFocus),
    onBlur: both(own.onBlur, triggerProps.onBlur),
    onClick: both(own.onClick, triggerProps.onClick),
    'aria-describedby': mounted ? id : own['aria-describedby'],
  });

  const host = container?.current;
  return (
    <>
      {trigger}
      {mounted &&
        host &&
        createPortal(
          <div ref={posRef} className="hx-tip-pos">
            <div ref={tipRef} id={id} className="hx-tip" role="tooltip" data-state={state}>
              {label}
              {keys?.map((key) => (
                <kbd key={key}>{key}</kbd>
              ))}
            </div>
          </div>,
          host,
        )}
    </>
  );
};

export default Tooltip;
