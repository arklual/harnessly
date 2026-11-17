// What the voice did and is doing («действия видны карточками у маскота»): every action of a plan — a page
// opened, a chat made, a model switched, an answer given, a task sent to an agent — is a line under the mascot, with
// its state: under way, waiting for an agent, done, failed. The full-screen talk shows the last few, the bar the last
// one; a line about a chat opens it. The call's end clears them.
import { useSyncExternalStore } from 'react';
import type { ConvId } from '@/next/protocol/v2';

export const ACT = { doing: 'doing', waiting: 'waiting', done: 'done', failed: 'failed' } as const;
export type TActState = (typeof ACT)[keyof typeof ACT];

export interface IVoiceAct {
  id: number;
  text: string;
  state: TActState;
  /** The chat it is about: the line opens it. */
  chat?: ConvId;
  at: number;
}

/** Lines kept: older ones fall off. */
export const ACTS_KEPT = 6;

let acts: readonly IVoiceAct[] = [];
let seq = 0;
const listeners = new Set<() => void>();
const emit = () => {
  for (const l of [...listeners]) l();
};

/** A new line; its id changes it later. */
export const addAct = (text: string, state: TActState = ACT.done, chat?: ConvId): number => {
  seq += 1;
  acts = [...acts, { id: seq, text, state, chat, at: Date.now() }].slice(-ACTS_KEPT);
  emit();
  return seq;
};

export const updateAct = (id: number, patch: Partial<Pick<IVoiceAct, 'text' | 'state' | 'chat'>>) => {
  if (!acts.some((a) => a.id === id)) return;
  acts = acts.map((a) => (a.id === id ? { ...a, ...patch, at: Date.now() } : a));
  emit();
};

/** The line waiting on a chat, if one is (its agent's answer closes it). */
export const actOfChat = (chat: ConvId): IVoiceAct | null =>
  acts.findLast((a) => a.chat === chat && (a.state === ACT.waiting || a.state === ACT.doing)) ?? null;

export const clearActs = () => {
  if (acts.length === 0) return;
  acts = [];
  emit();
};

export const voiceActs = (): readonly IVoiceAct[] => acts;

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};
export const useVoiceActs = (): readonly IVoiceAct[] =>
  useSyncExternalStore(subscribe, voiceActs, () => acts);
