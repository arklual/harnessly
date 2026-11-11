// The one reading of a chat's agent state on the page. The state lives in the chat's record of the list and nowhere
// else: the runtime owns it, the convs channel and the chat's own channel both bring it with the record's rev, and
// the merge rule (stores/conversation-merge) keeps the later of the two. The list, the tab, the feed, the counters
// and the notices read it here or off the same record, so they cannot disagree; the stores of the turn, of «now»
// and of the queue hold their own data and are not asked what the chat is doing.

import { conversationsStore, useConversation } from '@/next/stores/conversations';
import { mergeAgentState } from '@/next/stores/conversation-merge';
import type { AgentStamp, AgentState, ConvId, Turn } from '@/next/protocol/v2';

/** The states in which the agent is out of any turn: a turn the feed still holds live is over for the chat. */
export const OUT_OF_TURN = ['ready', 'idle', 'stopped', 'launch-failed', 'gone'] as const;
const outOfTurn: ReadonlySet<AgentState['k']> = new Set(OUT_OF_TURN);

// The chats whose own channel stamps the agent state. A runtime from before the stamps says nothing of the state
// there, and the feed of such a chat goes by its turns, as it did.
const stamped = new Set<ConvId>();

/** The chat's agent state; null while the list does not hold the chat. */
export const agentStateOf = (conv: ConvId): AgentState | null => conversationsStore.get(conv)?.state ?? null;

/** `agentStateOf` for a component. */
export const useAgentState = (conv: ConvId | null): AgentState | null => useConversation(conv)?.state ?? null;

/** Whether the chat's own channel stamps its agent state (false: a runtime from before the stamps). */
export const isStamped = (conv: ConvId): boolean => stamped.has(conv);

/**
 * What a snapshot of the chat's channel says of the agent state. `current`: the stamp is of the epoch the machine's
 * records count in now. No stamp: the runtime does not send them, and the chat goes back to the older reading.
 */
export function noteSnapshotStamp(conv: ConvId, stamp: AgentStamp | undefined, current: boolean): void {
  if (!stamp) {
    stamped.delete(conv);
    return;
  }
  noteStamp(conv, stamp, current);
}

/** A stamp an event of the chat's channel carried. */
export function noteStamp(conv: ConvId, stamp: AgentStamp, current: boolean): void {
  stamped.add(conv);
  if (current) mergeAgentState(conv, stamp);
}

/**
 * Whether a turn of the feed is the one the agent is in now: the feed draws its live work only then. A live turn of
 * a chat whose state says the agent is out of any turn is over, and its end is on its way in the channel.
 */
export function turnRuns(conv: ConvId, turn: Pick<Turn, 'status'>): boolean {
  if (turn.status !== 'live') return false;
  // The one branch for a runtime that does not stamp the state: the turn's own word, as before.
  if (!isStamped(conv)) return true;
  const state = agentStateOf(conv);
  return state === null || !outOfTurn.has(state.k);
}
