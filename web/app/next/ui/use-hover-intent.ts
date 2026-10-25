// The DOM side of hover intent (hover-intent.ts): pointer and focus handlers for a trigger and the element it opens,
// Esc and a press outside, touch (a tap toggles, a hover never happens), and nesting: a card opened from inside another
// keeps its parent open. The caller draws both elements and spreads the props; the exit is the caller's usePresence.
import {
  createContext,
  useContext,
  useEffect,
  useLayoutEffect,
  useState,
  type FocusEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react';
import {
  createHoverIntent,
  fitInViewport,
  HOVER_PART,
  rectOf,
  type IHoverIntent,
  type IHoverWarmth,
  type IRect,
} from '@/next/ui/hover-intent';

/** Elements a card stays open for while the pointer heads to or rests on them: menus, listboxes, modals, opt-ins. */
export const HOVER_KEEP = "[data-hover-keep], [role='menu'], [role='listbox'], [aria-modal='true']";

export interface IHoverParent {
  hold: () => void;
  release: () => void;
}

/** The card a nested card opens from: it stays open while the nested one is. */
export const HoverParentContext = createContext<IHoverParent | null>(null);

export interface IUseHoverIntentOptions {
  openDelay?: number;
  closeDelay?: number;
  restMs?: number;
  /** False for a hint: leaving the trigger closes it at once, a click closes it. */
  interactive?: boolean;
  warmth?: IHoverWarmth;
  /** Nothing opens (the trigger is busy, the card has nothing to say). */
  disabled?: boolean;
  onOpenChange?: (open: boolean) => void;
}

interface ITriggerProps {
  onPointerEnter: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerLeave: (event: ReactPointerEvent<HTMLElement>) => void;
  onFocus: (event: FocusEvent<HTMLElement>) => void;
  onBlur: (event: FocusEvent<HTMLElement>) => void;
  onClick: () => void;
}

interface IFloatingProps {
  onPointerEnter: (event: ReactPointerEvent<HTMLElement>) => void;
  onPointerLeave: (event: ReactPointerEvent<HTMLElement>) => void;
  onFocus: (event: FocusEvent<HTMLElement>) => void;
  onBlur: (event: FocusEvent<HTMLElement>) => void;
}

export interface IHoverIntentHandle {
  open: boolean;
  pinned: boolean;
  /** Callback refs for the two elements. */
  setTrigger: (el: HTMLElement | null) => void;
  setFloating: (el: HTMLElement | null) => void;
  /** The elements, for effects that measure them. */
  getTrigger: () => HTMLElement | null;
  getFloating: () => HTMLElement | null;
  triggerProps: ITriggerProps;
  floatingProps: IFloatingProps;
  close: () => void;
  /** For a card that opens cards of its own: wrap its content in HoverParentContext with this. */
  parent: IHoverParent;
}

const isTouch = (event: ReactPointerEvent) => event.pointerType === 'touch';

const focusVisible = (el: Element): boolean => {
  try {
    return el.matches(':focus-visible');
  } catch {
    return true;
  }
};

const keptEl = (node: EventTarget | null, trigger: Element | null): Element | null => {
  if (!(node instanceof Element)) return null;
  const el = node.closest(HOVER_KEEP);
  // A modal around the trigger itself keeps nothing: the card lives inside it.
  if (!el || (trigger && el.contains(trigger))) return null;
  return el;
};

const keptRects = (trigger: Element | null, floating: Element | null): IRect[] =>
  Array.from(document.querySelectorAll(HOVER_KEEP))
    .filter((el) => !(trigger && el.contains(trigger)) && !(floating && el.contains(floating)))
    .map(rectOf)
    .filter((r) => r.right > r.left && r.bottom > r.top);

/**
 * The binding of one trigger and its card to the state machine: plain object, made once per component, holding the
 * elements, the options of the last render and the window listeners of an open card.
 */
const createBinding = (
  initial: IUseHoverIntentOptions,
  setOpen: (open: boolean) => void,
  setPinned: (pinned: boolean) => void,
) => {
  let options = initial;
  let trigger: HTMLElement | null = null;
  let floating: HTMLElement | null = null;

  const intent: IHoverIntent = createHoverIntent({
    openDelay: initial.openDelay,
    closeDelay: initial.closeDelay,
    restMs: initial.restMs,
    interactive: initial.interactive,
    warmth: initial.warmth,
    rects: () => ({
      trigger: trigger ? rectOf(trigger) : null,
      floating: floating ? rectOf(floating) : null,
      kept: keptRects(trigger, floating),
    }),
    onChange: (next) => {
      setOpen(next);
      setPinned(intent.pinned);
      options.onOpenChange?.(next);
    },
  });

  const inside = (node: EventTarget | null): boolean =>
    node instanceof Node &&
    (trigger?.contains(node) === true || floating?.contains(node) === true || keptEl(node, trigger) !== null);

  const onBlur = (event: FocusEvent<HTMLElement>) => {
    if (!inside(event.relatedTarget)) intent.blur();
  };

  const triggerProps: ITriggerProps = {
    onPointerEnter: (event) => {
      if (!isTouch(event) && !options.disabled) intent.enter(HOVER_PART.trigger);
    },
    onPointerLeave: (event) => {
      if (!isTouch(event)) intent.leave(HOVER_PART.trigger, { x: event.clientX, y: event.clientY });
    },
    onFocus: (event) => {
      if (!options.disabled && focusVisible(event.currentTarget)) intent.focus();
    },
    onBlur,
    onClick: () => {
      if (options.interactive === false) intent.close();
      else if (!options.disabled) {
        intent.toggle();
        setPinned(intent.pinned);
      }
    },
  };

  const floatingProps: IFloatingProps = {
    onPointerEnter: (event) => {
      if (!isTouch(event)) intent.enter(HOVER_PART.floating);
    },
    onPointerLeave: (event) => {
      if (!isTouch(event)) intent.leave(HOVER_PART.floating, { x: event.clientX, y: event.clientY });
    },
    // Focus moving into the card keeps it: the trigger's blur saw it coming (relatedTarget inside).
    onFocus: () => undefined,
    onBlur,
  };

  /** While open: the pointer's way, Esc and a press outside. Returns the cleanup. */
  const listen = () => {
    const onMove = (event: PointerEvent) => {
      if (event.pointerType === 'touch') return;
      intent.move({ x: event.clientX, y: event.clientY }, keptEl(event.target, trigger) !== null);
    };
    const onDown = (event: PointerEvent) => {
      if (!inside(event.target)) intent.close();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') intent.close();
    };
    window.addEventListener('pointermove', onMove, { passive: true });
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  };

  return {
    intent,
    listen,
    triggerProps,
    floatingProps,
    update: (next: IUseHoverIntentOptions) => {
      options = next;
      if (next.disabled) intent.close();
    },
    setTrigger: (el: HTMLElement | null) => {
      trigger = el;
    },
    setFloating: (el: HTMLElement | null) => {
      floating = el;
    },
    getTrigger: () => trigger,
    getFloating: () => floating,
    close: () => intent.close(),
    parent: { hold: () => intent.hold(), release: () => intent.release() },
  };
};

export const useHoverIntent = (options: IUseHoverIntentOptions = {}): IHoverIntentHandle => {
  const [open, setOpen] = useState(false);
  const [pinned, setPinned] = useState(false);
  const parent = useContext(HoverParentContext);
  const [binding] = useState(() => createBinding(options, setOpen, setPinned));

  useLayoutEffect(() => {
    binding.update(options);
  });

  // A nested card holds its parent for as long as it is open.
  useEffect(() => {
    if (!open || !parent) return;
    parent.hold();
    return () => parent.release();
  }, [open, parent]);

  useEffect(() => (open ? binding.listen() : undefined), [open, binding]);
  useEffect(() => () => binding.intent.destroy(), [binding]);

  return {
    open,
    pinned,
    setTrigger: binding.setTrigger,
    setFloating: binding.setFloating,
    getTrigger: binding.getTrigger,
    getFloating: binding.getFloating,
    triggerProps: binding.triggerProps,
    floatingProps: binding.floatingProps,
    close: binding.close,
    parent: binding.parent,
  };
};

/**
 * Keeps an open element inside the viewport: the sideways shift goes to `--hover-dx`, a height that does not fit to
 * `--hover-max-h` with `data-fit-tall` (the stylesheet scrolls it). Measured when it opens.
 */
export const useFitInViewport = (get: () => HTMLElement | null, open: boolean) => {
  useLayoutEffect(() => {
    const el = get();
    if (!el || !open) return;
    el.style.removeProperty('--hover-dx');
    el.style.removeProperty('--hover-max-h');
    el.removeAttribute('data-fit-tall');
    const { dx, maxHeight } = fitInViewport(rectOf(el), {
      width: window.innerWidth,
      height: window.innerHeight,
    });
    if (dx !== 0) el.style.setProperty('--hover-dx', `${Math.round(dx)}px`);
    if (maxHeight !== null) {
      el.style.setProperty('--hover-max-h', `${Math.floor(maxHeight)}px`);
      el.setAttribute('data-fit-tall', 'true');
    }
  }, [get, open]);
};
