import { useEffect, useRef, useState, type RefObject } from 'react';
import { PHONE_QUERY } from '@/next/features/shell/gestures';
import { usePresentFlag, PRESENCE, type TPresenceState } from '@/next/ui/presence';

/** The pointer rests on the rail this long before the sidebar opens over the window: passing by does not open it. */
export const PEEK_OPEN_MS = 280;
/** Leaving it, the sidebar waits this long for the pointer to come back before it folds. */
export const PEEK_GRACE_MS = 240;
/** The fold back to the rail, as long as its clip's animation in tabs.css (hx-peek-out). */
export const PEEK_EXIT_MS = 220;

/** Something of the sidebar is open in the portal (a row's menu, the profile's): the peek holds while it is. */
const HELD = '.hx-portal [role="menu"], .hx-portal [role="dialog"]';

export type TPeek = TPresenceState | null;

/**
 * The folded sidebar opens over the window while the pointer rests on it, and folds back when the pointer has left it
 * for a moment, as a dock does. Only with a mouse and on a wide window; a menu opened from it, a field being typed in
 * or the keyboard's focus inside hold it open. Escape folds it. `rail` false (the toggle pinned it open) ends it at once.
 */
const useRailPeek = (app: RefObject<HTMLElement | null>, rail: boolean): TPeek => {
  const [open, setOpen] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  if (!rail && open) setOpen(false);

  useEffect(() => {
    const sidebar = app.current?.querySelector<HTMLElement>('.hx-sidebar');
    if (!rail || !sidebar) return;
    const clear = () => {
      if (timer.current !== null) clearTimeout(timer.current);
      timer.current = null;
    };
    const held = () =>
      document.querySelector(HELD) !== null ||
      (sidebar.contains(document.activeElement) &&
        document.activeElement?.matches('input, textarea') === true);
    const fold = () => {
      clear();
      timer.current = setTimeout(function check() {
        if (held()) {
          timer.current = setTimeout(check, PEEK_GRACE_MS);
          return;
        }
        timer.current = null;
        setOpen(false);
      }, PEEK_GRACE_MS);
    };
    const onEnter = (event: PointerEvent) => {
      if (event.pointerType !== 'mouse' || matchMedia(PHONE_QUERY).matches) return;
      clear();
      timer.current = setTimeout(() => setOpen(true), PEEK_OPEN_MS);
    };
    const onLeave = (event: PointerEvent) => {
      if (event.pointerType !== 'mouse') return;
      fold();
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      clear();
      setOpen(false);
    };
    sidebar.addEventListener('pointerenter', onEnter);
    sidebar.addEventListener('pointerleave', onLeave);
    document.addEventListener('keydown', onKey);
    return () => {
      clear();
      sidebar.removeEventListener('pointerenter', onEnter);
      sidebar.removeEventListener('pointerleave', onLeave);
      document.removeEventListener('keydown', onKey);
    };
  }, [app, rail]);

  const { mounted, state } = usePresentFlag(rail && open, PEEK_EXIT_MS);
  if (!rail || !mounted) return null;
  return state === PRESENCE.closing ? PRESENCE.closing : PRESENCE.open;
};

export default useRailPeek;
