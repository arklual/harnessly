// The chats our agent delegated to other agents (the runtime's delegate_task, internal/delegation): each stands as a
// card under the turn that started it, with the chat's live state, and opens on a click.
import { createContext } from 'react';
import type { Conversation } from '@/next/protocol/v2';

/** The chats this chat delegated, oldest first; empty for a chat that delegated none. */
export const DelegatedChats = createContext<readonly Conversation[]>([]);

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
