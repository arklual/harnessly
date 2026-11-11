// The one way a chat becomes read: it is on screen in a window the person sees, and what its agent finished is marked
// read on its machine. The machine's `conversation_seen` goes to every tab and device (and the hub's copy), the
// sidebar's mark and the notification centre's row go with it. Until that word comes the mark is sent again: a command
// lost on the way (a timeout of a busy machine, a link that dropped) would else leave the chat unread for good.
import { useEffect, useSyncExternalStore } from 'react';
import { markSeen } from '@/next/commands';
import type { ConvId, Conversation, MachineId } from '@/next/protocol/v2';
import { isUnread } from '@/next/stores/conversations';

/** The waits between the marks sent while the chat stays unread; the last one repeats. */
export const MARK_AGAIN_MS = [1_500, 4_000, 10_000, 30_000] as const;

interface ITimers {
  set: (fn: () => void, ms: number) => number;
  clear: (id: number) => void;
}

const WINDOW_TIMERS: ITimers = {
  set: (fn, ms) => window.setTimeout(fn, ms),
  clear: (id) => window.clearTimeout(id),
};

/**
 * Sends `send` now and again after each wait of MARK_AGAIN_MS until stopped. The caller stops it when the record says
 * the chat is read (or it is not on screen any more); returns the stop.
 */
export const keepMarking = (send: () => void, timers: ITimers = WINDOW_TIMERS): (() => void) => {
  let attempt = 0;
  let timer = 0;
  let live = true;
  const run = () => {
    if (!live) return;
    send();
    timer = timers.set(run, MARK_AGAIN_MS[Math.min(attempt, MARK_AGAIN_MS.length - 1)]);
    attempt += 1;
  };
  run();
  return () => {
    live = false;
    timers.clear(timer);
  };
};

/** Up to when the chat is to be read: its last finished turn, by the machine's own clock; null when nothing is unread. */
export const readUpToOf = (conv: Conversation | null): number | null =>
  conv && isUnread(conv) ? (conv.lastTurn?.endedAt ?? null) : null;

const subscribeVisible = (listener: () => void) => {
  document.addEventListener('visibilitychange', listener);
  window.addEventListener('focus', listener);
  return () => {
    document.removeEventListener('visibilitychange', listener);
    window.removeEventListener('focus', listener);
  };
};
const visibleNow = () => document.visibilityState === 'visible';

/** The window is seen: the tab is in front and the app not hidden. */
export const useWindowVisible = (): boolean =>
  useSyncExternalStore(subscribeVisible, visibleNow, () => false);

/** Marks `conv` read while it is on screen in a seen window and has an unread turn; again until the machine says so. */
export const useReadWhileVisible = (conv: Conversation | null): void => {
  const visible = useWindowVisible();
  const upTo = readUpToOf(conv);
  const machine: MachineId | null = conv?.machineId ?? null;
  const id: ConvId | null = conv?.id ?? null;
  useEffect(() => {
    if (!visible || machine === null || id === null || upTo === null) return;
    return keepMarking(() => void markSeen(machine, id, upTo));
  }, [visible, machine, id, upTo]);
};
