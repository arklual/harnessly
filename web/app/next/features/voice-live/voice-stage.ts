// The stage of the full-screen talk: while the voice talks, anything can be shown to the person on the whole
// area over the mascot (a card, a board, a list) — showOnVoiceStage(<Card />, 'Заголовок'). The mascot shrinks and steps
// down to the bottom meanwhile; null clears the stage and it comes back (voice-full.tsx draws it).
import { useSyncExternalStore, type ReactNode } from 'react';

export interface IVoiceStage {
  node: ReactNode;
  title: string | null;
}

let current: IVoiceStage | null = null;
const listeners = new Set<() => void>();

/** Shows `node` on the stage with an optional title; null (or nothing to show) clears it. */
export const showOnVoiceStage = (node: ReactNode | null, title?: string) => {
  const next: IVoiceStage | null =
    node === null || node === undefined || node === false ? null : { node, title: title ?? null };
  if (next === null && current === null) return;
  current = next;
  for (const listen of listeners) listen();
};

export const voiceStage = (): IVoiceStage | null => current;

const subscribe = (listen: () => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};

export const useVoiceStage = (): IVoiceStage | null =>
  useSyncExternalStore(subscribe, voiceStage, () => null);
