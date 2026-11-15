// A reload ends the talk honestly: the microphone and the voice's connection do not survive the page. The
// chat the talk was in says so once — «Разговор прервался · Продолжить» — and one click starts the voice again in it;
// the chat's history is where it was, and the voice reads its last lines when it starts.
import { useSyncExternalStore } from 'react';

/** The talk going on, kept for the page that comes after a reload (sessionStorage: this tab only). */
const KEY = 'hx-voice-talk';

const readKept = (): string | null => {
  try {
    const kept = window.sessionStorage.getItem(KEY);
    window.sessionStorage.removeItem(KEY);
    return kept;
  } catch {
    return null;
  }
};

/** The chat whose talk the last reload cut; null once it is continued, dismissed or another talk starts. */
let cut: string | null = typeof window === 'undefined' ? null : readKept();
const listeners = new Set<() => void>();
const set = (next: string | null) => {
  if (cut === next) return;
  cut = next;
  for (const listen of [...listeners]) listen();
};

/** A talk started in this chat (null: it ended as asked): kept so a reload knows it cut one. */
export const keepTalk = (conv: string | null) => {
  try {
    if (conv) window.sessionStorage.setItem(KEY, conv);
    else window.sessionStorage.removeItem(KEY);
  } catch {
    // No storage (a private window): a reload says nothing.
  }
  if (conv) set(null);
};

/** The reload's note is answered: continued or put away. */
export const dropCutTalk = () => set(null);

/** True while this chat's talk was cut by a reload and not continued yet. */
export const useCutTalk = (conv: string): boolean =>
  useSyncExternalStore(
    (listen) => {
      listeners.add(listen);
      return () => listeners.delete(listen);
    },
    () => cut === conv,
    () => false,
  );
