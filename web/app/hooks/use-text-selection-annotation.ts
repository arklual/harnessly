import { useCallback, useEffect, useState } from 'react';
import {
  ANNOTATE_MESSAGE_EVENT,
  messageRootOf,
  readMessageTarget,
  readSelectionTarget,
  targetKey,
  type ISelectionTarget,
} from '@/lib/hl-selection-target';

export const POPOVER_ATTRIBUTE = 'data-annotation-popover';
const KEY_DELAY_MS = 120;
const TOUCH_DELAY_MS = 300;

const inPopover = (node: EventTarget | Element | null): boolean =>
  node instanceof Element && node.closest(`[${POPOVER_ATTRIBUTE}]`) !== null;

const sameTarget = (a: ISelectionTarget | null, b: ISelectionTarget | null): boolean =>
  a === b ||
  (a !== null &&
    b !== null &&
    targetKey(a) === targetKey(b) &&
    a.rect.left === b.rect.left &&
    a.rect.right === b.rect.right &&
    a.rect.firstTop === b.rect.firstTop &&
    a.rect.lastBottom === b.rect.lastBottom);

/**
 * The fragment selected inside the quotable messages of `scope`, for the panel next to it. A mouse selection
 * is read when the button is released, a touch or keyboard one after it settles; the panel goes away with the selection
 * unless focus is inside it (a click on its buttons must not collapse the fragment it was opened for).
 */
const useTextSelectionAnnotation = (scope: HTMLElement | null) => {
  const [target, setTarget] = useState<ISelectionTarget | null>(null);
  const [dismissedKey, setDismissedKey] = useState<string | null>(null);

  const dismiss = useCallback(() => {
    if (target) setDismissedKey(targetKey(target));
    setTarget(null);
  }, [target]);

  useEffect(() => {
    if (!scope) return;
    let timer = 0;
    let dragging = false;
    const refresh = () => {
      if (dragging || inPopover(document.activeElement)) return;
      let next = readSelectionTarget(scope, window.getSelection());
      if (next && targetKey(next) === dismissedKey) next = null;
      else if (dismissedKey !== null) setDismissedKey(null);
      setTarget((current) => (current?.fromButton || sameTarget(current, next) ? current : next));
    };
    const schedule = (delay: number) => {
      window.clearTimeout(timer);
      timer = window.setTimeout(refresh, delay);
    };
    const onSelectionChange = () => {
      if (!dragging) schedule(window.matchMedia('(pointer: coarse)').matches ? TOUCH_DELAY_MS : KEY_DELAY_MS);
    };
    const onPointerDown = (event: PointerEvent) => {
      if (inPopover(event.target)) return;
      if (event.pointerType === 'touch') {
        setTarget((current) => (current?.fromButton ? null : current));
        return;
      }
      if (event.button !== 0) return;
      dragging = true;
      window.clearTimeout(timer);
      setTarget(null);
    };
    const onPointerEnd = () => {
      if (!dragging) return;
      dragging = false;
      schedule(0);
    };
    const onScroll = () => schedule(0);
    const onAnnotateMessage = (event: Event) => {
      const root = messageRootOf(event.target);
      const next = root && (readSelectionTarget(scope, window.getSelection()) ?? readMessageTarget(root));
      if (next) setTarget(next);
    };
    document.addEventListener('selectionchange', onSelectionChange);
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('pointerup', onPointerEnd, true);
    document.addEventListener('pointercancel', onPointerEnd, true);
    scope.addEventListener('scroll', onScroll, true);
    scope.addEventListener(ANNOTATE_MESSAGE_EVENT, onAnnotateMessage);
    window.addEventListener('resize', onScroll);
    return () => {
      window.clearTimeout(timer);
      document.removeEventListener('selectionchange', onSelectionChange);
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('pointerup', onPointerEnd, true);
      document.removeEventListener('pointercancel', onPointerEnd, true);
      scope.removeEventListener('scroll', onScroll, true);
      scope.removeEventListener(ANNOTATE_MESSAGE_EVENT, onAnnotateMessage);
      window.removeEventListener('resize', onScroll);
    };
  }, [scope, dismissedKey]);

  return { target, dismiss };
};

export default useTextSelectionAnnotation;
