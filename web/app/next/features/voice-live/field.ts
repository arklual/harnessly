// The voice is a state of the field it was started from («Голос — в поле»): the field draws the wave on its
// text line and the ring by who has the floor, and what is typed while the voice is on goes to the voice. One voice at a
// time; the field knows it is its own by the element the voice's button stands in.
import { useSyncExternalStore, type RefObject } from 'react';
import type { THeadKind } from '@/next/features/voice-live/api';
import { setVoiceMood, VOICE_MOOD } from '@/next/features/voice-live/voice-mood';
import { showOnVoiceStage } from '@/next/features/voice-live/voice-stage';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export const FIELD_FLOOR = {
  listening: 'listening',
  starting: 'starting',
  thinking: 'thinking',
  speaking: 'speaking',
  muted: 'muted',
  failed: 'failed',
} as const;
export type TFieldFloor = (typeof FIELD_FLOOR)[keyof typeof FIELD_FLOOR];

/** One turn of the call as the feed of the text mode shows it: who, what, and whether it is still going. */
export interface IVoiceTurn {
  role: 'user' | 'assistant';
  text: string;
  done: boolean;
}

export interface IVoiceField {
  /** The field (.hx-composer) the voice stands in now; null while its chat is not on screen (the voice goes on). */
  host: HTMLElement | null;
  /** The chat the talk belongs to (voice-layer.tsx): it lives while the person goes elsewhere. */
  conv: ConvId | null;
  machine: MachineId;
  /** Ends the talk (the bar's «Завершить»). */
  end: () => void;
  floor: TFieldFloor;
  /** Loudness of whoever speaks now, 0…1; null while nobody does. */
  level: (() => number) | null;
  /** What the person is saying now, as it is heard. */
  heard: string;
  /** What the voice is saying now, as it goes (the line of its reply). */
  said: string;
  /** The turns of the call so far, in order (the text mode's feed); empty while the talk writes nothing down. */
  turns?: readonly IVoiceTurn[];
  /** Typed words go to the voice, which answers out loud. */
  typed: (text: string) => void;
  /** The voice agent's look (Settings). */
  head: THeadKind;
  /** The person spoke over the voice and it is silenced (barge.ts): the head shows «barged». */
  interrupted: boolean;
  /** The microphone is off (the full-screen talk's button and the Space key). */
  muted: boolean;
  /** Turns the microphone off and on. */
  mute: () => void;
}

let current: IVoiceField | null = null;
const listeners = new Set<() => void>();

export const setVoiceField = (next: IVoiceField | null) => {
  // A talk that begins opens the full-screen layer (voice-full.tsx); folding it is the person's, until the talk ends.
  if (current === null && next !== null) expanded = true;
  if (next === null) {
    expanded = false;
    // What the talk showed leaves with it.
    showOnVoiceStage(null);
    setVoiceMood(VOICE_MOOD.neutral);
  }
  current = next;
  for (const listen of listeners) listen();
};

let expanded = false;

/** True while the full-screen talk is open: the layer stands over the chat, the bar waits. */
export const voiceExpanded = (): boolean => expanded && current !== null;

/** Opens (true) or folds (false) the full-screen talk; the call itself goes on. */
export const setVoiceExpanded = (on: boolean) => {
  const next = on && current !== null;
  if (next === expanded) return;
  expanded = next;
  for (const listen of listeners) listen();
};

export const useVoiceExpanded = (): boolean => useSyncExternalStore(subscribe, voiceExpanded, () => false);

const subscribe = (listen: () => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};

/** The voice of this field, null when it has none. */
export const useVoiceField = (root: RefObject<HTMLElement | null>): IVoiceField | null => {
  const field = useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );
  return field && root.current && field.host === root.current ? field : null;
};

/** The voice that is on now, read once (⌥ Space during a talk goes back to its chat). */
export const voiceNow = (): IVoiceField | null => current;

/** The voice that is on now, wherever its field is: the sidebar's row and the bar away from its chat. */
export const useVoiceNow = (): IVoiceField | null =>
  useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  );

/** The voice's place moved between its chat's field and the layer's hidden place (voice-layer.tsx). */
export const VOICE_MOVED_EVENT = 'hx-voice-moved';

/** The event a field's voice listens to: ⌥ Space asked it to start. */
export const VOICE_START_EVENT = 'hx-voice-start';
let startAsked = false;

/** ⌥ Space: the field on screen starts its voice; a field that comes a moment later (a chat being opened) starts too. */
export const askVoiceStart = () => {
  startAsked = true;
  window.dispatchEvent(new Event(VOICE_START_EVENT));
  window.setTimeout(() => {
    startAsked = false;
  }, 4000);
};

/** True once after ⌥ Space asked: the field that takes it starts its voice. */
export const takeVoiceStart = (): boolean => {
  const asked = startAsked;
  startAsked = false;
  return asked;
};

/** How the voice of a chat looks from afar (the sidebar's row, the bar): who has the floor, in three. */
export const VOICE_LOOK = { speak: 'speak', listen: 'listen', think: 'think' } as const;
export type TVoiceLook = (typeof VOICE_LOOK)[keyof typeof VOICE_LOOK];

/** The look of a floor: the agent speaks (accent), the person (red), the agent thinks or the voice starts (grey). */
export const lookOf = (floor: TFieldFloor): TVoiceLook => {
  if (floor === FIELD_FLOOR.speaking) return VOICE_LOOK.speak;
  if (floor === FIELD_FLOOR.listening) return VOICE_LOOK.listen;
  return VOICE_LOOK.think;
};

/** The look of the voice of this chat, null when the talk is not its: a row re-renders only when that changes. */
export const useVoiceLookOf = (conv: string): TVoiceLook | null =>
  useSyncExternalStore(
    subscribe,
    () => (current?.conv === conv ? lookOf(current.floor) : null),
    () => null,
  );
// The wake phrase (wake.ts, wake-word.tsx). A voice is on in some field: the phrase is not listened for meanwhile.
export const useVoiceOn = (): boolean =>
  useSyncExternalStore(
    subscribe,
    () => current !== null,
    () => false,
  );

let wakeListening = false;
const wakeListeners = new Set<() => void>();

/** The page listens for «Привет, harnessly» now (the microphone is open for it): the head may show it waits. */
export const setWakeListening = (on: boolean) => {
  if (wakeListening === on) return;
  wakeListening = on;
  for (const listen of wakeListeners) listen();
};

export const useWakeListening = (): boolean =>
  useSyncExternalStore(
    (listen) => {
      wakeListeners.add(listen);
      return () => wakeListeners.delete(listen);
    },
    () => wakeListening,
    () => false,
  );

let wakeStart = false;

/** The voice about to start was asked by the phrase, not a button: it ends itself on silence or «пока». */
export const markWakeStart = () => {
  wakeStart = true;
  window.setTimeout(() => {
    wakeStart = false;
  }, 4000);
};

/** True once for the voice the phrase started. */
export const takeWakeStart = (): boolean => {
  const was = wakeStart;
  wakeStart = false;
  return was;
};
