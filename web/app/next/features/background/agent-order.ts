// The order of the subagents in the right column and how many of them show before «Ещё N»: the ones still running come
// first, then the most recently started; a long list shows only its first few rows until the person opens the rest.
import type { BackgroundTask } from '@/next/protocol/v2';

/** The rows of subagents shown before the fold. */
export const AGENTS_FIRST = 5;

/** How high a row stands: at work, then an idle teammate (alive, waiting for a message), then the ones over. */
const RANK = { running: 2, idle: 1, done: 0, stopped: 0, failed: 0 } as const satisfies Record<
  BackgroundTask['status'],
  number
>;

/** Running first, then idle teammates, then the newest start; a copy, the store's list is not touched. */
export const orderAgents = (tasks: readonly BackgroundTask[]): BackgroundTask[] =>
  [...tasks].sort((a, b) => RANK[b.status] - RANK[a.status] || b.startedAt - a.startedAt);

/** The rows that show at once and the rest behind the fold. */
export const splitAgents = (
  tasks: readonly BackgroundTask[],
  first: number = AGENTS_FIRST,
): { head: BackgroundTask[]; rest: BackgroundTask[] } => {
  const ordered = orderAgents(tasks);
  return { head: ordered.slice(0, first), rest: ordered.slice(first) };
};
