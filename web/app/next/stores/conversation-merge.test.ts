import { beforeEach, describe, expect, it } from 'vitest';
import { setScheduler } from '@/next/core/store';
import { conversationsStore } from '@/next/stores/conversations';
import {
  amendConversation,
  FROM_CACHE,
  FROM_CENTRAL,
  fromLive,
  fromSnapshot,
  mergeAgentState,
  mergeConversation,
  mergeConversationFields,
  mergeMachineList,
  originOf,
  removeConversation,
  SOURCE,
  stateVersionOf,
  veilConversation,
} from '@/next/stores/conversation-merge';
import type { AgentStamp, ConvId, Conversation, MachineId } from '@/next/protocol/v2';

const M = 'm-1' as MachineId;
const ID = 'tab-1' as ConvId;

const chat = (over: Partial<Omit<Conversation, 'id'>> & { id?: string } = {}) =>
  ({
    id: ID,
    machineId: M,
    title: 'title',
    rev: 1,
    lastActivityAt: 100,
    seenAt: 0,
    state: { k: 'idle' },
    ...over,
  }) as unknown as Conversation;

const held = () => conversationsStore.get(ID)!;

describe('the one rule of the conversation list', () => {
  beforeEach(() => {
    setScheduler('sync');
    for (const id of [...conversationsStore.keys()]) conversationsStore.delete(id);
  });

  it('remembers where a record came from', () => {
    mergeConversation(chat(), fromLive('e1'));
    expect(originOf(held())).toEqual({ src: SOURCE.live, epoch: 'e1' });
    // A record that entered the store past the rule is trusted no more than the cache.
    conversationsStore.set(ID, chat());
    expect(originOf(held()).src).toBe(SOURCE.cache);
  });

  it('within an epoch the machine word is taken by rev only', () => {
    mergeConversation(chat({ rev: 5, title: 'five' }), fromLive('e1'));
    mergeConversation(chat({ rev: 5, title: 'again' }), fromSnapshot('e1'));
    mergeConversation(chat({ rev: 4, title: 'older' }), fromLive('e1'));
    expect(held().title).toBe('five');
    mergeConversation(chat({ rev: 6, title: 'six' }), fromLive('e1'));
    expect(held().title).toBe('six');
  });

  it('a runtime that started over is taken whatever the revs', () => {
    mergeConversation(chat({ rev: 50, title: 'before' }), fromLive('e1'));
    mergeConversation(chat({ rev: 1, title: 'after' }), fromSnapshot('e2'));
    expect(held().title).toBe('after');
    expect(originOf(held())).toEqual({ src: SOURCE.snapshot, epoch: 'e2' });
  });

  it('a copy with the same time does not replace the live record', () => {
    const live = mergeConversation(chat({ rev: 3, state: { k: 'working' } as never }), fromLive('e1'));
    const after = mergeConversation(chat({ rev: 0, title: 'stale copy' }), FROM_CENTRAL);
    expect(after).toBe(live);
    expect(held()).toBe(live);
    // An older copy neither.
    mergeConversation(chat({ rev: 0, lastActivityAt: 99, title: 'older copy' }), FROM_CENTRAL);
    expect(held()).toBe(live);
  });

  it('a copy that knows of later activity replaces the live record, and the machine replaces it back', () => {
    mergeConversation(chat({ rev: 3 }), fromLive('e1'));
    mergeConversation(chat({ rev: 0, lastActivityAt: 101, title: 'copy' }), FROM_CENTRAL);
    expect(held().title).toBe('copy');
    expect(originOf(held()).src).toBe(SOURCE.central);
    // The copy carries no version of the machine: its next word wins at any rev.
    mergeConversation(chat({ rev: 2, title: 'machine' }), fromLive('e1'));
    expect(held().title).toBe('machine');
  });

  it('the machine word replaces the copy and the cache whatever revs they kept', () => {
    for (const from of [FROM_CENTRAL, FROM_CACHE]) {
      conversationsStore.delete(ID);
      mergeConversation(chat({ rev: 99, title: 'kept' }), from);
      mergeConversation(chat({ rev: 1, title: 'machine' }), fromSnapshot('e1'));
      expect(held().title).toBe('machine');
    }
  });

  it('the cache only fills what is missing', () => {
    mergeConversation(chat({ title: 'copy' }), FROM_CENTRAL);
    mergeConversation(chat({ lastActivityAt: 500, title: 'cache' }), FROM_CACHE);
    expect(held().title).toBe('copy');
    mergeConversation(chat({ id: 'tab-2', title: 'cache' }), FROM_CACHE);
    expect(conversationsStore.get('tab-2' as ConvId)?.title).toBe('cache');
  });

  it('the copy replaces the cache unless the cache is newer, and replaces itself always', () => {
    mergeConversation(chat({ lastActivityAt: 200, title: 'cache' }), FROM_CACHE);
    mergeConversation(chat({ lastActivityAt: 150, title: 'copy' }), FROM_CENTRAL);
    expect(held().title).toBe('cache');
    mergeConversation(chat({ lastActivityAt: 200, title: 'copy' }), FROM_CENTRAL);
    expect(held().title).toBe('copy');
    mergeConversation(chat({ lastActivityAt: 150, title: 'the last answer' }), FROM_CENTRAL);
    expect(held().title).toBe('the last answer');
  });

  it('an event of some fields goes by the same rule', () => {
    mergeConversation(chat({ rev: 3 }), fromLive('e1'));
    const before = held();
    mergeConversationFields(ID, 3, fromLive('e1'), () => ({ title: 'replayed' }));
    expect(held()).toBe(before);
    mergeConversationFields(ID, 4, fromLive('e1'), () => ({ title: 'new' }));
    expect(held()).toMatchObject({ title: 'new', rev: 4 });
    // A chat the list does not hold stays absent.
    mergeConversationFields('tab-9' as ConvId, 1, fromLive('e1'), () => ({ title: 'x' }));
    expect(conversationsStore.has('tab-9' as ConvId)).toBe(false);
  });

  it('a field that came live over a copy is not taken back by the same copy', () => {
    mergeConversation(chat({ rev: 0 }), FROM_CENTRAL);
    mergeConversationFields(ID, 2, fromLive('e1'), () => ({ state: { k: 'working' } as never }));
    mergeConversation(chat({ rev: 0 }), FROM_CENTRAL);
    expect(held().state.k).toBe('working');
    expect(originOf(held()).src).toBe(SOURCE.live);
  });

  it('an amendment keeps the origin and the version', () => {
    mergeConversation(chat({ rev: 3 }), fromLive('e1'));
    amendConversation(ID, (c) => ({ ...c, title: 'amended' }));
    expect(held()).toMatchObject({ title: 'amended', rev: 3 });
    expect(originOf(held())).toEqual({ src: SOURCE.live, epoch: 'e1' });
    const before = held();
    amendConversation(ID, (c) => c);
    expect(held()).toBe(before);
  });

  it('a look the page drew over a record gives way to the machine at the same rev, not to the copy', () => {
    mergeConversation(chat({ rev: 3, title: 'live' }), fromLive('e1'));
    veilConversation(ID, (c) => ({ ...c, state: { k: 'stopped' } as never }));
    expect(originOf(held())).toEqual({ src: SOURCE.live, epoch: 'e1', veiled: true });
    mergeConversation(chat({ rev: 0, title: 'copy' }), FROM_CENTRAL);
    expect(held()).toMatchObject({ title: 'live', state: { k: 'stopped' } });
    mergeConversation(chat({ rev: 3, title: 'back' }), fromSnapshot('e1'));
    expect(held()).toMatchObject({ title: 'back', state: { k: 'idle' } });
    expect(originOf(held()).veiled).toBeUndefined();
  });

  it('the machine list is merged in one update: its chats are never all gone on the way', () => {
    for (const id of ['tab-1', 'tab-2', 'tab-3']) mergeConversation(chat({ id, rev: 7 }), FROM_CACHE);
    mergeConversation(chat({ id: 'tab-other', machineId: 'm-2' as MachineId }), FROM_CACHE);
    const seen: string[][] = [];
    const off = conversationsStore.subscribeAll(() =>
      seen.push(
        [...conversationsStore.values()]
          .filter((c) => c.machineId === M)
          .map((c) => c.id)
          .sort(),
      ),
    );
    const from = fromSnapshot('e1');
    mergeMachineList(M, [chat({ id: 'tab-2', rev: 1 }), chat({ id: 'tab-4', rev: 1 })], (c) =>
      mergeConversation(c, from),
    );
    off();
    expect(seen).toEqual([['tab-2', 'tab-4']]);
    expect(conversationsStore.has('tab-other' as ConvId)).toBe(true);
    expect(originOf(conversationsStore.get('tab-2' as ConvId)!).src).toBe(SOURCE.snapshot);
  });
});

const stamp = (k: string, rev: number, epoch = 'e1') =>
  ({ state: { k }, rev, epoch }) as unknown as AgentStamp;

describe("the agent state of the chat's own channel", () => {
  beforeEach(() => {
    setScheduler('sync');
    for (const id of [...conversationsStore.keys()]) removeConversation(id);
  });

  it('a later stamp changes the state and leaves the record at its rev', () => {
    mergeConversation(chat({ rev: 5 }), fromLive('e1'));
    mergeAgentState(ID, stamp('working', 7));
    expect(held()).toMatchObject({ rev: 5, state: { k: 'working' } });
    expect(stateVersionOf(held())).toEqual({ epoch: 'e1', rev: 7 });
    expect(originOf(held())).toEqual({ src: SOURCE.live, epoch: 'e1' });
  });

  it('a stamp that is not later than the state held changes nothing', () => {
    mergeConversation(chat({ rev: 5 }), fromLive('e1'));
    const before = held();
    mergeAgentState(ID, stamp('working', 5));
    mergeAgentState(ID, stamp('working', 4));
    expect(held()).toBe(before);
    mergeAgentState(ID, stamp('working', 8));
    mergeAgentState(ID, stamp('ready', 7));
    expect(held().state.k).toBe('working');
    expect(stateVersionOf(held()).rev).toBe(8);
  });

  it('a record that comes between keeps the later state and brings its other fields', () => {
    mergeConversation(chat({ rev: 5 }), fromLive('e1'));
    mergeAgentState(ID, stamp('working', 8));
    mergeConversation(chat({ rev: 6, title: 'renamed' }), fromLive('e1'));
    expect(held()).toMatchObject({ rev: 6, title: 'renamed', state: { k: 'working' } });
    mergeConversationFields(ID, 7, fromLive('e1'), () => ({ state: { k: 'ready' } as never }));
    expect(held()).toMatchObject({ rev: 7, state: { k: 'working' } });
    expect(stateVersionOf(held()).rev).toBe(8);
  });

  it('a record at the stamp rev or later is the state from then on', () => {
    mergeConversation(chat({ rev: 5 }), fromLive('e1'));
    mergeAgentState(ID, stamp('working', 8));
    mergeConversation(chat({ rev: 9, state: { k: 'ready' } as never }), fromLive('e1'));
    expect(held().state.k).toBe('ready');
    expect(stateVersionOf(held())).toEqual({ epoch: 'e1', rev: 9 });
    mergeAgentState(ID, stamp('working', 8));
    expect(held().state.k).toBe('ready');
  });

  it('a fact without a version keeps the stamp unless it changes the state itself', () => {
    mergeConversation(chat({ rev: 5 }), fromLive('e1'));
    mergeAgentState(ID, stamp('working', 8));
    amendConversation(ID, (c) => ({ ...c, title: 'amended' }));
    expect(stateVersionOf(held()).rev).toBe(8);
    mergeAgentState(ID, stamp('ready', 7));
    expect(held()).toMatchObject({ title: 'amended', state: { k: 'working' } });
  });

  it('a runtime that started over is taken whatever the revs', () => {
    mergeConversation(chat({ rev: 50 }), fromLive('e1'));
    mergeAgentState(ID, stamp('working', 60));
    mergeAgentState(ID, stamp('ready', 2, 'e2'));
    expect(held().state.k).toBe('ready');
    expect(stateVersionOf(held())).toEqual({ epoch: 'e2', rev: 2 });
    // The new runtime's record replaces the old one's and its state stands by the new epoch's revs.
    mergeConversation(chat({ rev: 1, state: { k: 'unknown' } as never }), fromSnapshot('e2'));
    expect(held().state.k).toBe('ready');
    mergeConversation(chat({ rev: 3, state: { k: 'working' } as never }), fromLive('e2'));
    expect(held().state.k).toBe('working');
  });

  it('the machine state replaces the copy, the cache and the look of the page at any rev', () => {
    mergeConversation(chat({ rev: 40 }), FROM_CENTRAL);
    mergeAgentState(ID, stamp('working', 3));
    expect(held()).toMatchObject({ rev: 40, state: { k: 'working' } });
    expect(originOf(held()).src).toBe(SOURCE.central);
    mergeAgentState(ID, stamp('ready', 2));
    expect(held().state.k).toBe('working');

    removeConversation(ID);
    mergeConversation(chat({ rev: 40 }), fromLive('e1'));
    veilConversation(ID, (c) => ({ ...c, state: { k: 'stopped' } as never }));
    mergeAgentState(ID, stamp('working', 3));
    expect(held().state.k).toBe('working');
  });

  it('the copy that knows of later activity drops the stamp with the record', () => {
    mergeConversation(chat({ rev: 5, lastActivityAt: 100 }), fromLive('e1'));
    mergeAgentState(ID, stamp('working', 8));
    mergeConversation(chat({ rev: 2, lastActivityAt: 200, state: { k: 'stopped' } as never }), FROM_CENTRAL);
    expect(held().state.k).toBe('stopped');
    expect(stateVersionOf(held())).toEqual({ epoch: null, rev: 2 });
  });

  it('a stamp that comes before the record waits for it', () => {
    mergeAgentState(ID, stamp('working', 8));
    mergeAgentState(ID, stamp('idle', 6));
    expect(conversationsStore.has(ID)).toBe(false);
    mergeConversation(chat({ rev: 7 }), fromSnapshot('e1'));
    expect(held()).toMatchObject({ rev: 7, state: { k: 'working' } });
    expect(stateVersionOf(held()).rev).toBe(8);
  });

  it('what was read stays read: a copy that has not heard of the mark keeps its other fields, not its seenAt', () => {
    const turn = { id: 't1', status: 'done', endedAt: 150 };
    mergeConversation(chat({ rev: 5, lastTurn: turn, seenAt: 150 } as never), fromLive('e1'));
    // The hub's copy knows of later activity but was written before the mark came.
    mergeConversation(
      chat({ rev: 4, lastActivityAt: 200, title: 'copy', lastTurn: turn, seenAt: 0 } as never),
      FROM_CENTRAL,
    );
    expect(held()).toMatchObject({ title: 'copy', seenAt: 150 });
    // A machine's snapshot asked before the mark: the same.
    mergeConversation(chat({ rev: 6, lastTurn: turn, seenAt: 100 } as never), fromSnapshot('e2'));
    expect(held().seenAt).toBe(150);
    mergeConversationFields(ID, 7, fromLive('e2'), () => ({ seenAt: 90 }));
    expect(held().seenAt).toBe(150);
    // A later mark moves it on.
    mergeConversationFields(ID, 8, fromLive('e2'), () => ({ seenAt: 300 }));
    expect(held().seenAt).toBe(300);
  });
});
