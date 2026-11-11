// The order in which the messages of one chat leave the page: one at a time, in the order they were written. The next
// one goes only when the runtime has answered for the one before it (it has it, or refused it on the merits), so a
// message whose try was lost on the road is never overtaken by a later one. The runtime orders its queue by the same
// clock — the UUIDv7 of the clientId — so the copy a cluster delivers falls into the same order.

import { convMessagesStore, messagesStore, type MessageView } from '@/next/stores/messages';
import type { ClientId, ConvId, MachineId } from '@/next/protocol/v2';

const V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7/i;

/** When the page made the message, by its clientId (a UUIDv7 opens with 48 bits of ms); null for any other id. */
export const madeAt = (id: string): number | null =>
  V7.test(id) ? Number.parseInt(id.slice(0, 8) + id.slice(9, 13), 16) : null;

/**
 * Written earlier first, by the clock of the clientId. Messages of one millisecond, or with ids that say nothing of
 * their time, compare equal: a stable sort keeps them in the order the page holds them, which is the order they were
 * made in.
 */
export function byMaking(a: MessageView, b: MessageView): number {
  const x = madeAt(a.clientId);
  const y = madeAt(b.clientId);
  return x !== null && y !== null ? x - y : 0;
}

/** The sends the runtime answered «I have it» whose message has not come back as an event yet. */
const answered = new Set<ClientId>();

export const noteAnswered = (id: ClientId): void => void answered.add(id);
export const forgetAnswered = (id: ClientId): void => void answered.delete(id);

/** The page's own message that still has to reach the runtime: not shown by it, not answered, not taken back. */
export const isUnsent = (m: MessageView): boolean =>
  m.rev === 0 &&
  m.state.k === 'submitting' &&
  !m.cancelling &&
  m.conversationId !== null &&
  !answered.has(m.clientId);

/** The chat's unsent messages of this machine, the earliest written first. */
function unsentOf(machine: MachineId, conv: ConvId): MessageView[] {
  const out: MessageView[] = [];
  for (const id of convMessagesStore.get(conv) ?? []) {
    const m = messagesStore.get(id);
    if (m && m.machineId === machine && m.conversationId === conv && isUnsent(m)) out.push(m);
  }
  return out.sort(byMaking);
}

/** The message whose turn it is to go, null when the chat has nothing unsent. */
export const nextUnsent = (machine: MachineId, conv: ConvId): MessageView | null =>
  unsentOf(machine, conv)[0] ?? null;

/** m waits for an earlier message of its chat to be answered first. */
export function waitsItsTurn(m: MessageView): boolean {
  if (!m.conversationId || !isUnsent(m)) return false;
  const first = nextUnsent(m.machineId, m.conversationId);
  return first !== null && first.clientId !== m.clientId;
}

/** For tests. */
export function resetSendLine(): void {
  answered.clear();
}
