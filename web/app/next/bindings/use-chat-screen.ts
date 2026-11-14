// A chat remembers its full screen (stores/chat-screen.ts): a screen opened from a chat is kept as that chat's; the chat
// itself, opened again, puts the screen back — unless the person has just come from that screen to the chat, which is
// leaving it. The screen may arrive after the chat opened (the view from another device, stores/chat-view.ts): then it
// is put back the same way — only right after the chat opened and before the person did anything in it: a
// view that comes later never moves a page the person is already in.
import { useEffect, useRef } from 'react';
import { forgetScreen, keepScreen, keptScreen, screenOf } from '@/next/stores/chat-screen';
import { onViewApplied } from '@/next/stores/chat-view';
import { goTo, ROUTE, type TRoute } from '@/next/routes/routes';

/** How long after a chat opened a screen arriving from another device still takes it there. */
export const LATE_VIEW_MS = 1500;

/** When the person last pressed a key or a pointer on the page. */
let lastInputAt = 0;
const noteInput = () => {
  lastInputAt = Date.now();
};

export const useChatScreen = (route: TRoute) => {
  const from = useRef<TRoute | null>(null);
  /** The chat the person has just left a screen for: its screen stays forgotten while they are in it. */
  const left = useRef<string | null>(null);
  const now = useRef(route);
  /** The chat on screen and when it was opened. */
  const opened = useRef<{ id: string | null; at: number }>({ id: null, at: 0 });
  useEffect(() => {
    const id = route.kind === ROUTE.chat ? route.id : null;
    if (id !== opened.current.id) opened.current = { id, at: Date.now() };
    now.current = route;
  }, [route]);
  useEffect(() => {
    window.addEventListener('pointerdown', noteInput, true);
    window.addEventListener('keydown', noteInput, true);
    return () => {
      window.removeEventListener('pointerdown', noteInput, true);
      window.removeEventListener('keydown', noteInput, true);
    };
  }, []);
  useEffect(() => {
    const was = from.current;
    from.current = route;
    const screen = screenOf(route);
    if (screen) {
      left.current = null;
      keepScreen(screen.conv, screen.path);
      return;
    }
    if (route.kind !== ROUTE.chat) {
      left.current = null;
      return;
    }
    if (was !== null && screenOf(was)?.conv === route.id) {
      left.current = route.id;
      forgetScreen(route.id);
      return;
    }
    const kept = keptScreen(route.id);
    if (kept) goTo(kept, { replace: true });
  }, [route]);
  useEffect(
    () =>
      onViewApplied(() => {
        const at = now.current;
        if (at.kind !== ROUTE.chat || left.current === at.id) return;
        // Late: the person is in the chat already; the screen waits for the next time it is opened.
        const since = opened.current.id === at.id ? opened.current.at : 0;
        if (Date.now() - since > LATE_VIEW_MS || lastInputAt > since) return;
        const kept = keptScreen(at.id);
        if (kept) goTo(kept, { replace: true });
      }),
    [],
  );
};
