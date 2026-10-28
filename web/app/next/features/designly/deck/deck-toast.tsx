// The deck's own notice, as the board draws it: one dark pill with a check at the bottom centre of the deck, 2.4 s, the
// next one replaces it. It tells what the deck started (an export, the agent's shortening); refusals and failures go
// to the app's toasts (ui/toast) with their reason.
import { useSyncExternalStore } from 'react';
import Icon from '@/next/ui/icon';

/** The pill's whole life: its keyframes fade it in and out inside this time. */
export const DECK_TOAST_MS = 2400;

interface IDeckNotice {
  id: number;
  text: string;
}

let notice: IDeckNotice | null = null;
let seq = 0;
let timer: ReturnType<typeof setTimeout> | null = null;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((listener) => listener());

/** Shows the deck's notice. */
export const deckToast = (text: string): void => {
  notice = { id: ++seq, text };
  if (timer) clearTimeout(timer);
  timer = setTimeout(() => {
    notice = null;
    timer = null;
    emit();
  }, DECK_TOAST_MS);
  emit();
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

/** Drawn once by the deck's shell. */
const DeckToast = () => {
  const shown = useSyncExternalStore(
    subscribe,
    () => notice,
    () => null,
  );
  if (shown === null) return null;
  return (
    <div key={shown.id} className="hx-deck-toast" role="status">
      <Icon name="check" size="sm" />
      {shown.text}
    </div>
  );
};

export default DeckToast;
