// The order of the queue's messages: which rows can be carried, and what putting one in another place asks of the
// machine. Pure: the pointer and the DOM are use-drag's, the command is the owner's. The machine keeps the order and
// refuses what breaks it, so these rules only keep the page from asking for what it knows will be refused.
import type { QueueRow } from '@/next/features/composer/types';
import { EDGE, type TEdge } from '@/next/ui/use-drag';

/** The kind of what is carried and of where it goes: the queue's own, no other list takes its rows. */
export const QUEUED = 'queued';
export const QUEUED_KINDS: readonly (typeof QUEUED)[] = [QUEUED];

/** The words of a drag: put the message `id` before the message `beforeId`, or last when that is null. */
export interface IMovePlan {
  id: string;
  beforeId: string | null;
}

export const DIRECTION = { up: -1, down: 1 } as const;
export type TDirection = (typeof DIRECTION)[keyof typeof DIRECTION];

/** A row the machine holds in its line and can put elsewhere: not one on its way to the agent, not a failed one. */
export const isMovable = (row: QueueRow): boolean => row.state.k === 'queued' && !row.steer;

/** How many rows can change places: with fewer than two there is nothing to put in another order. */
export const movableCount = (rows: readonly QueueRow[]): number => rows.filter(isMovable).length;

/**
 * The rows in the machine's order. The machine's line (`items`, the head first) is the truth: the rows that stand in it
 * fill the places they hold, in its order, and the rows that are not in it (on their way, failed) keep their own.
 */
export const inQueueOrder = (
  rows: readonly QueueRow[],
  items: readonly string[] | null | undefined,
): QueueRow[] => {
  if (!items || items.length === 0) return [...rows];
  const place = new Map(items.map((id, index) => [id, index]));
  const inLine = rows.filter((row) => place.has(row.clientId));
  inLine.sort((a, b) => (place.get(a.clientId) ?? 0) - (place.get(b.clientId) ?? 0));
  let next = 0;
  return rows.map((row) => (place.has(row.clientId) ? inLine[next++] : row));
};

/**
 * What putting `sourceId` before or after `targetId` asks for; null when it asks for nothing: the row is not one that
 * moves, the target is not one it can stand beside, the place is its own, or it would stand ahead of a message sent
 * «now», which keeps its place at the head.
 */
export const planMove = (
  rows: readonly QueueRow[],
  sourceId: string,
  targetId: string,
  edge: TEdge,
): IMovePlan | null => {
  const line = rows.filter(isMovable);
  const source = line.find((row) => row.clientId === sourceId);
  const target = line.find((row) => row.clientId === targetId);
  if (!source || !target || source === target || source.ahead) return null;
  const rest = line.filter((row) => row !== source);
  const at = rest.indexOf(target) + (edge === EDGE.after ? 1 : 0);
  if (at < rest.filter((row) => row.ahead).length) return null;
  if (at === line.indexOf(source)) return null;
  return { id: sourceId, beforeId: rest[at]?.clientId ?? null };
};

/** One place up or down, for the keyboard: null at the end of the line, or where the machine would refuse. */
export const planStep = (rows: readonly QueueRow[], id: string, direction: TDirection): IMovePlan | null => {
  const line = rows.filter(isMovable);
  const at = line.findIndex((row) => row.clientId === id);
  const neighbour = at < 0 ? undefined : line[at + direction];
  if (!neighbour) return null;
  return planMove(rows, id, neighbour.clientId, direction === DIRECTION.up ? EDGE.before : EDGE.after);
};
