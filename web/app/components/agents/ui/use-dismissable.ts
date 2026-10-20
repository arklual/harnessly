import { useEffectEvent, useLayoutEffect, useRef, type RefObject } from 'react';

/**
 * Closing behaviour shared by every floating layer: Escape, a pointer outside, focus leaving,
 * and handing focus back to whatever opened the layer.
 *
 * Layers form one stack (a submenu sits above its menu, a popover above the dialog it was
 * opened from). A pointer press walks the stack from the top: every layer the press is outside
 * of closes, the walk stops at the first layer that contains it. So a click in a menu closes only
 * its open submenu, and a click on a trigger never closes-and-reopens its own layer.
 *
 * Escape is handled on the layer element itself and stops there, so window-level shortcuts
 * (the permission prompt answers "n" on Escape) never see an Escape a layer consumed.
 * A control inside a layer that owns Escape (a combobox closing its list) marks itself with
 * data-tw-escape="self" and the layer leaves that Escape alone.
 */

export const DISMISS_REASON = {
  escape: 'escape',
  outside: 'outside',
  focusOutside: 'focus-outside',
} as const;

export type TDismissReason = (typeof DISMISS_REASON)[keyof typeof DISMISS_REASON];

export interface IDismissableOptions {
  /** The layer is open. Default true. */
  enabled?: boolean;
  /** Presses on the trigger are "inside": the trigger toggles the layer itself. */
  triggerRef?: RefObject<HTMLElement | null>;
  /** Escape pressed inside the layer closes it. Default true. */
  escape?: boolean;
  /** A pointer press outside closes it. Default true. */
  outside?: boolean;
  /** Focus moving to an element outside closes it (non-modal popovers and menus). Default false. */
  focusOutside?: boolean;
  /** Give focus back to the trigger (or to what had focus on open) when the layer closes. Default true. */
  restoreFocus?: boolean;
  /** A modal layer: a press outside it never reaches the layers underneath. */
  modal?: boolean;
  /** The layer's own scrim (sheet backdrop, dialog backdrop): a click on it closes this layer only. */
  scrimRef?: RefObject<HTMLElement | null>;
}

interface ILayerEntry {
  element: () => HTMLElement | null;
  trigger: () => HTMLElement | null;
  scrim: () => HTMLElement | null;
  dismiss: (reason: TDismissReason) => void;
  outside: boolean;
  focusOutside: boolean;
  modal: boolean;
}

const stack: ILayerEntry[] = [];

const contains = (entry: ILayerEntry, target: Node) =>
  Boolean(entry.element()?.contains(target) || entry.trigger()?.contains(target));

const eventTarget = (event: Event): Node | null => {
  const [first] = event.composedPath();
  return first instanceof Node ? first : null;
};

const onPointerDown = (event: PointerEvent) => {
  const target = eventTarget(event);
  if (!target) return;
  for (const entry of [...stack].reverse()) {
    if (contains(entry, target)) return;
    // A scrim closes its layer on click (see useDismissable): closing on press would let the
    // rest of the tap land on whatever lies under the scrim, e.g. reopen the trigger.
    if (entry.scrim()?.contains(target)) return;
    if (entry.outside) entry.dismiss(DISMISS_REASON.outside);
    if (entry.modal) return;
  }
};

const onFocusIn = (event: FocusEvent) => {
  const target = eventTarget(event);
  if (!target || target === document.body) return;
  for (const entry of [...stack].reverse()) {
    if (contains(entry, target)) return;
    if (entry.modal) return;
    if (entry.focusOutside) entry.dismiss(DISMISS_REASON.focusOutside);
  }
};

const push = (entry: ILayerEntry) => {
  if (!stack.length) {
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('focusin', onFocusIn, true);
  }
  stack.push(entry);
};

const remove = (entry: ILayerEntry) => {
  const index = stack.indexOf(entry);
  if (index !== -1) stack.splice(index, 1);
  if (!stack.length) {
    document.removeEventListener('pointerdown', onPointerDown, true);
    document.removeEventListener('focusin', onFocusIn, true);
  }
};

/** True while `element` belongs to the top layer, so a nested child layer keeps its own keys. */
export const isTopLayer = (element: HTMLElement | null): boolean => {
  const top = stack[stack.length - 1];
  return Boolean(element && top && top.element() === element);
};

/** Number of open layers. Modal layers use it to paint above everything opened before them. */
export const openLayerCount = (): number => stack.length;

const FOCUS_OPTIONS = { preventScroll: true } as const;

export const useDismissable = (
  ref: RefObject<HTMLElement | null>,
  onClose: (reason: TDismissReason) => void,
  options: IDismissableOptions = {},
): void => {
  const {
    enabled = true,
    triggerRef,
    escape = true,
    outside = true,
    focusOutside = false,
    restoreFocus = true,
    modal = false,
    scrimRef,
  } = options;
  const lastReason = useRef<TDismissReason | null>(null);
  const close = useEffectEvent((reason: TDismissReason) => {
    lastReason.current = reason;
    onClose(reason);
  });

  useLayoutEffect(() => {
    if (!enabled) return;
    lastReason.current = null;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const trigger = triggerRef?.current ?? null;
    const entry: ILayerEntry = {
      element: () => ref.current,
      trigger: () => triggerRef?.current ?? null,
      scrim: () => scrimRef?.current ?? null,
      dismiss: (reason) => close(reason),
      outside,
      focusOutside,
      modal,
    };
    push(entry);

    const element = ref.current;
    const depth = String(stack.length - 1);
    element?.style.setProperty('--tw-ui-depth', depth);
    scrimRef?.current?.style.setProperty('--tw-ui-depth', depth);
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.isComposing || event.defaultPrevented) return;
      const target = event.target instanceof Element ? event.target : null;
      const owner = target?.closest('[data-tw-escape="self"]');
      if (owner && element?.contains(owner)) return;
      event.preventDefault();
      event.stopPropagation();
      close(DISMISS_REASON.escape);
    };
    if (escape) element?.addEventListener('keydown', onKeyDown);
    const scrim = scrimRef?.current ?? null;
    const onScrimClick = () => close(DISMISS_REASON.outside);
    scrim?.addEventListener('click', onScrimClick);

    return () => {
      element?.removeEventListener('keydown', onKeyDown);
      scrim?.removeEventListener('click', onScrimClick);
      remove(entry);
      if (!restoreFocus) return;
      const reason = lastReason.current;
      if (reason === DISMISS_REASON.outside || reason === DISMISS_REASON.focusOutside) return;
      const active = document.activeElement;
      const focusWasInside = !active || active === document.body || Boolean(element?.contains(active));
      if (!focusWasInside) return;
      const target = trigger ?? previous;
      if (!target?.isConnected) return;
      // A modal layer lifts `inert` from the page in a cleanup that runs after this one: focus once it is gone.
      if (target.closest('[inert]')) queueMicrotask(() => target.focus(FOCUS_OPTIONS));
      else target.focus(FOCUS_OPTIONS);
    };
  }, [enabled, escape, focusOutside, modal, outside, ref, restoreFocus, scrimRef, triggerRef]);
};
