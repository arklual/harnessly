import type { AgentConversation } from '@/lib/hl-conversations';

interface IFleetList {
  conversations: AgentConversation[];
  errors?: { machineId: string; error: string }[];
}

/**
 * A machine that did not answer one round (a NetBird reconnect, a slow scan) keeps its conversations from
 * the previous answer: a list that loses them blinks rows away and back. A machine that stays silent is marked
 * unavailable by the sidebar once its error lasts; a machine gone from the fleet stops being listed as an error.
 */
export const keepUnansweredMachines = <T extends IFleetList>(previous: T | null, next: T): T => {
  if (!previous || !next.errors?.length) return next;
  const silent = new Set(next.errors.map((error) => error.machineId));
  const shown = new Set(next.conversations.map((item) => `${item.machineId ?? ''}:${item.id}`));
  const kept = previous.conversations.filter(
    (item) => item.machineId && silent.has(item.machineId) && !shown.has(`${item.machineId}:${item.id}`),
  );
  return kept.length ? { ...next, conversations: [...next.conversations, ...kept] } : next;
};
