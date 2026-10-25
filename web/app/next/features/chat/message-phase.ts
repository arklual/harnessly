// What a message of the person looks like while it goes out (spec §1, §12): pale «sending» at once, «takes longer
// than usual» after 3 s without a confirmation. A message never turns red because of the road: without a network, or
// while the machine does not answer (restart, gateway, no reply), it waits, calmly, and says so; the page sends it
// again by itself. Red is for what the runtime refused on the merits. The store owns the state; this only reads it
// against the clock.
import { useEffect, useState } from 'react';
import type { LocalMessageState, Ms } from '@/next/features/chat/types';

export const SEND_SLOW_MS = 3000;

export const MESSAGE_PHASE = {
  draft: 'draft',
  sending: 'sending',
  slow: 'slow',
  waitingNet: 'waiting-net',
  /** The machine did not answer (or is not there): the message waits for it and goes by itself. */
  waitingMachine: 'waiting-machine',
  queued: 'queued',
  /** Typed into a working agent: in its queue, read at its next step. */
  handed: 'handed',
  sent: 'sent',
  failed: 'failed',
  cancelled: 'cancelled',
} as const;

export type TMessagePhase = (typeof MESSAGE_PHASE)[keyof typeof MESSAGE_PHASE];

/** The states in which the page still waits for the runtime to confirm the message. */
const isGoingOut = (state: LocalMessageState): boolean =>
  state.k === 'uploading' || state.k === 'submitting' || state.k === 'typing' || state.k === 'in-input';

/**
 * What to show for a message in `state` since `since`, at `now`. `handed`: the message is in the working agent's queue
 * — it waits for the agent's step, which may take minutes, so it is neither slow nor stuck.
 */
export const messagePhase = (
  state: LocalMessageState,
  since: Ms,
  now: Ms,
  offline: boolean,
  handed = false,
  waitsMachine = false,
): TMessagePhase => {
  if (state.k === 'draft') return MESSAGE_PHASE.draft;
  if (state.k === 'queued') return MESSAGE_PHASE.queued;
  if (state.k === 'failed') return MESSAGE_PHASE.failed;
  if (state.k === 'cancelled') return MESSAGE_PHASE.cancelled;
  // Accepted without its entry: in the agent's own queue, not read yet — pending like a steer.
  if (handed && state.k === 'accepted' && state.entryId === null) return MESSAGE_PHASE.handed;
  if (!isGoingOut(state)) return MESSAGE_PHASE.sent;
  if (handed && state.k === 'in-input') return MESSAGE_PHASE.handed;
  // Before the runtime has it, a lost network is the reason; once it types, the machine is.
  if (offline && (state.k === 'submitting' || state.k === 'uploading')) return MESSAGE_PHASE.waitingNet;
  if (waitsMachine && state.k === 'submitting') return MESSAGE_PHASE.waitingMachine;
  // «Not confirmed» is the runtime's to say, by its own deadline (failed{unconfirmed}); the page has no term of its own.
  if (now - since >= SEND_SLOW_MS) return MESSAGE_PHASE.slow;
  return MESSAGE_PHASE.sending;
};

/** In how many ms the phase of a waiting message changes by itself; null when only a new state changes it. */
export const nextPhaseIn = (
  state: LocalMessageState,
  since: Ms,
  now: Ms,
  offline: boolean,
  handed = false,
  waitsMachine = false,
): number | null => {
  const phase = messagePhase(state, since, now, offline, handed, waitsMachine);
  if (phase === MESSAGE_PHASE.sending) return since + SEND_SLOW_MS - now;
  return null;
};

/** The phase, re-read exactly when a threshold passes: one timer per waiting message, none for the rest. */
export const useMessagePhase = (
  state: LocalMessageState,
  since: Ms,
  offline: boolean,
  handed = false,
  waitsMachine = false,
): TMessagePhase => {
  const [now, setNow] = useState(() => Date.now());
  const wait = nextPhaseIn(state, since, now, offline, handed, waitsMachine);
  useEffect(() => {
    if (wait === null) return;
    const timer = setTimeout(() => setNow(Date.now()), Math.max(0, wait));
    return () => clearTimeout(timer);
  }, [wait]);
  return messagePhase(state, since, now, offline, handed, waitsMachine);
};
