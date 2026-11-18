// The chats our agent delegated to other agents (the runtime's delegate_task, internal/delegation): each stands as a
// card under the turn that started it, with the chat's live state, and opens on a click.
import { createContext } from 'react';
import type { Conversation, MadeFrom, MadeItem, TurnOutput } from '@/next/protocol/v2';

/** The chats this chat delegated, oldest first; empty for a chat that delegated none. */
export const DelegatedChats = createContext<readonly Conversation[]>([]);

/** What the delegated chats made, each thing marked with the subchat it came through. */
export const SubchatsMade = createContext<readonly MadeItem[]>([]);
/** What the chat's helpers made, by the key of the chat's turn it shows in (bindings/inherited-made.ts liftedByTurn). */
export const LiftedMade = createContext<ReadonlyMap<string, readonly ILiftedOutput[]>>(new Map());

/** A thing a helper made, lifted into a turn of the chat: who made it, for the row's mark «из подчата …». */
export interface ILiftedOutput extends TurnOutput {
  via: MadeFrom;
}

/** Who made a card of «Сделано за ход» when it is no work of the turn itself; null for the turn's own. */
export const viaOf = (output: TurnOutput): MadeFrom | null =>
  'via' in output ? ((output as ILiftedOutput).via ?? null) : null;

/** Each subchat's chat further down (its subchat, theirs…) that waits for the person, by the subchat's id. */
export const WaitingBelow = createContext<ReadonlyMap<string, Conversation>>(new Map());

const WAITING: ReadonlySet<string> = new Set(['waiting-question', 'waiting-permission']);
/** How deep below a subchat the waiting chats are looked for (a loop of parents stops sooner). */
const BELOW_DEPTH = 8;

/**
 * For each chat `parent` delegated, the first chat below it (not itself) that waits for a question or a permission:
 * the person sits in `parent`, and a grandchild's question is theirs to answer.
 */
export const waitingBelow = (
  all: readonly Conversation[],
  parent: string,
): ReadonlyMap<string, Conversation> => {
  const kids = new Map<string, Conversation[]>();
  for (const c of all) {
    if (c.parentId === undefined) continue;
    const list = kids.get(c.parentId);
    if (list) list.push(c);
    else kids.set(c.parentId, [c]);
  }
  const out = new Map<string, Conversation>();
  for (const child of kids.get(parent) ?? []) {
    const seen = new Set([parent, child.id]);
    let level = kids.get(child.id) ?? [];
    for (let d = 0; d < BELOW_DEPTH && level.length > 0 && !out.has(child.id); d++) {
      const next: Conversation[] = [];
      for (const c of level) {
        if (seen.has(c.id)) continue;
        seen.add(c.id);
        if (WAITING.has(c.state.k)) {
          out.set(child.id, c);
          break;
        }
        next.push(...(kids.get(c.id) ?? []));
      }
      level = next;
    }
  }
  return out;
};

/** The delegated chats a turn started: created while it ran (a live turn runs on). */
export const startedIn = (
  chats: readonly Conversation[],
  startedAt: number,
  endedAt: number | null,
): readonly Conversation[] =>
  chats.filter((c) => c.createdAt >= startedAt && (endedAt === null || c.createdAt <= endedAt));

/** Where a delegated chat stands, as its card says it. */
export const CHILD_STATE = {
  starting: 'starting',
  working: 'working',
  waiting: 'waiting',
  done: 'done',
  stopped: 'stopped',
  failed: 'failed',
} as const;
export type TChildState = (typeof CHILD_STATE)[keyof typeof CHILD_STATE];

/** The state of a delegated chat from its record. */
export const childState = (c: Conversation): TChildState => {
  switch (c.state.k) {
    case 'starting':
    case 'unknown':
      return CHILD_STATE.starting;
    case 'working':
      return CHILD_STATE.working;
    case 'waiting-question':
    case 'waiting-permission':
      return CHILD_STATE.waiting;
    case 'stopped':
      return CHILD_STATE.stopped;
    case 'launch-failed':
    case 'gone':
      return CHILD_STATE.failed;
    case 'idle':
    case 'ready':
      return c.bgAgents ? CHILD_STATE.working : CHILD_STATE.done;
  }
};
