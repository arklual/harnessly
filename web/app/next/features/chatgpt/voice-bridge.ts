// The voice of a ChatGPT chat lives in the app's voice layer (voice-layer.tsx), as an agent chat's does: the talk goes
// on while the person opens the settings, a design or another chat («Голос — в другом чате»). The chat's
// screen and its voice tell each other through here: which model the screen picked, what the voice sent, what it kept.
import type { ConvId } from '@/next/protocol/v2';

/** The layer and the bar know a talk by its chat: a ChatGPT chat's id is marked so it is never taken for an agent's. */
const GPT_CONV_PREFIX = 'gpt:';

export const gptConv = (id: string): ConvId => `${GPT_CONV_PREFIX}${id}` as ConvId;

/** The ChatGPT chat a talk belongs to, or null for an agent's chat. */
export const gptIdOf = (conv: string): string | null =>
  conv.startsWith(GPT_CONV_PREFIX) ? conv.slice(GPT_CONV_PREFIX.length) : null;

export interface IGptPick {
  model: string;
  effort: string;
}

/** The model and depth the chat's screen shows picked: the voice writes with them (empty: the chat's own). */
const picks = new Map<string, IGptPick>();
export const setGptPick = (id: string, pick: IGptPick | null) => {
  if (pick) picks.set(id, pick);
  else picks.delete(id);
};
export const gptPickOf = (id: string): IGptPick => picks.get(id) ?? { model: '', effort: '' };

export const GPT_VOICE_EVENT = {
  /** The voice sent the chat a message: the screen follows the answer being written. */
  sent: 'sent',
  /** The voice kept what was said as the chat's messages: the screen reads the chat again. */
  kept: 'kept',
} as const;
export type TGptVoiceEvent = (typeof GPT_VOICE_EVENT)[keyof typeof GPT_VOICE_EVENT];

const listeners = new Map<string, Set<(event: TGptVoiceEvent) => void>>();

export const tellGptScreen = (id: string, event: TGptVoiceEvent) => {
  for (const listen of [...(listeners.get(id) ?? [])]) listen(event);
};

/** The chat's screen hears its voice; the returned function stops it. */
export const onGptVoice = (id: string, listen: (event: TGptVoiceEvent) => void): (() => void) => {
  const set = listeners.get(id) ?? new Set();
  set.add(listen);
  listeners.set(id, set);
  return () => {
    set.delete(listen);
    if (set.size === 0) listeners.delete(id);
  };
};
