import { describe, expect, it } from 'vitest';
import { CHILD_STATE, childState, startedIn } from '@/next/features/chat/delegation';
import type { Conversation } from '@/next/protocol/v2';

const chat = (id: string, createdAt: number, state: Conversation['state'], bgAgents = 0): Conversation =>
  ({ id, createdAt, state, bgAgents }) as unknown as Conversation;

describe('startedIn', () => {
  const chats = [
    chat('a', 100, { k: 'idle', since: 0 }),
    chat('b', 200, { k: 'idle', since: 0 }),
    chat('c', 300, { k: 'idle', since: 0 }),
  ];
  it('takes the chats created while the turn ran', () => {
    expect(startedIn(chats, 150, 250).map((c) => c.id)).toEqual(['b']);
  });
  it('runs on to now for a live turn', () => {
    expect(startedIn(chats, 150, null).map((c) => c.id)).toEqual(['b', 'c']);
  });
});

describe('childState', () => {
  it('reads an idle chat as done, unless its background agents still run', () => {
    expect(childState(chat('a', 0, { k: 'idle', since: 0 }))).toBe(CHILD_STATE.done);
    expect(childState(chat('a', 0, { k: 'idle', since: 0 }, 2))).toBe(CHILD_STATE.working);
  });
  it('reads a question or a permission as waiting', () => {
    expect(childState(chat('a', 0, { k: 'waiting-question' } as Conversation['state']))).toBe(
      CHILD_STATE.waiting,
    );
  });
});
