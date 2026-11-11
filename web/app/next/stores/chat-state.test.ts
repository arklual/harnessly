import { beforeEach, describe, expect, it } from 'vitest';
import { applyConvEvent, applyConvsEvent } from '@/next/core/realtime/apply';
import { setScheduler } from '@/next/core/store';
import { chatStatusOf } from '@/next/features/sidebar/status';
import { CHAT_STATUS } from '@/next/features/sidebar/types';
import { agentStateOf, isStamped, noteSnapshotStamp, OUT_OF_TURN, turnRuns } from '@/next/stores/chat-state';
import { conversationsByActivity, conversationsStore } from '@/next/stores/conversations';
import {
  fromSnapshot,
  mergeConversation,
  removeConversation,
  stateVersionOf,
} from '@/next/stores/conversation-merge';
import { machinesStore } from '@/next/stores/machines';
import type {
  AgentStamp,
  ConvEvent,
  ConvId,
  Conversation,
  ConvsEvent,
  MachineId,
  Queue,
} from '@/next/protocol/v2';

const M = 'm-1' as MachineId;
const ID = 'tab-race' as ConvId;
const EPOCH = 'e1';

const chat = (rev: number, k: string, title = 'title') =>
  ({
    id: ID,
    machineId: M,
    title,
    rev,
    lastActivityAt: 100,
    seenAt: 0,
    gate: null,
    lastTurn: null,
    state: { k, since: rev },
  }) as unknown as Conversation;

const queue = { conversationId: ID, items: [], paused: null, rev: 1 } as unknown as Queue;
const stamp = (k: string, rev: number, epoch = EPOCH) =>
  ({ state: { k, since: rev }, rev, epoch }) as unknown as AgentStamp;

// What the runtime did, by the record's revs: the state changed at 2, 4 and 5; rev 3 renamed the chat.
const CHANGES = [
  { rev: 1, k: 'idle' },
  { rev: 2, k: 'working' },
  { rev: 4, k: 'waiting-question' },
  { rev: 5, k: 'ready' },
] as const;
/** The state the runtime held at a rev. */
const stateAt = (rev: number): string => {
  let k = '';
  for (const change of CHANGES) if (change.rev <= rev) k = change.k;
  return k;
};

type Step = { ch: 'convs'; e: ConvsEvent } | { ch: 'conv'; e: ConvEvent };
const fromConvs = (e: ConvsEvent): Step => ({ ch: 'convs', e });
const fromConv = (agent: AgentStamp): Step => ({ ch: 'conv', e: { type: 'queue_changed', queue, agent } });

// The same history as each channel tells it: the list coalesces (rev 5 comes as a whole record), the chat's own
// channel stamps every change of the state.
const CONVS: Step[] = [
  fromConvs({ type: 'conversation_state', id: ID, state: chat(2, 'working').state, rev: 2 }),
  fromConvs({ type: 'conversation_upsert', conv: chat(3, 'working', 'renamed') }),
  fromConvs({ type: 'conversation_state', id: ID, state: chat(4, 'waiting-question').state, rev: 4 }),
  fromConvs({ type: 'conversation_upsert', conv: chat(5, 'ready', 'renamed') }),
];
const CONV: Step[] = [
  fromConv(stamp('working', 2)),
  fromConv(stamp('waiting-question', 4)),
  fromConv(stamp('ready', 5)),
];

function permutations<T>(items: readonly T[]): T[][] {
  if (items.length <= 1) return [[...items]];
  return items.flatMap((item, i) =>
    permutations([...items.slice(0, i), ...items.slice(i + 1)]).map((rest) => [item, ...rest]),
  );
}

/** Every way the two channels can interleave while each keeps its own order. */
function interleavings<T>(a: readonly T[], b: readonly T[]): T[][] {
  if (a.length === 0) return [[...b]];
  if (b.length === 0) return [[...a]];
  return [
    ...interleavings(a.slice(1), b).map((rest) => [a[0], ...rest]),
    ...interleavings(a, b.slice(1)).map((rest) => [b[0], ...rest]),
  ];
}

function apply(step: Step): void {
  if (step.ch === 'convs') applyConvsEvent(M, step.e);
  else applyConvEvent(M, ID, step.e);
}

/** What each place of the page reads of the chat now. */
function readings() {
  const listed = conversationsByActivity().find((c) => c.id === ID)!;
  const tab = conversationsStore.get(ID)!;
  const feed = agentStateOf(ID)!;
  return {
    list: chatStatusOf(listed, true),
    tab: chatStatusOf(tab, true),
    feed: feed.k,
    record: tab,
  };
}

function start(): void {
  removeConversation(ID);
  noteSnapshotStamp(ID, undefined, false);
  mergeConversation(chat(1, 'idle'), fromSnapshot(EPOCH));
}

/** Applies the steps and checks, after each, that every place reads one state and that it never went back. */
function play(steps: readonly Step[], label: string): void {
  start();
  let version = stateVersionOf(conversationsStore.get(ID)!).rev;
  for (const step of steps) {
    apply(step);
    const now = readings();
    const at = stateVersionOf(now.record);
    expect(at.rev, label).toBeGreaterThanOrEqual(version);
    version = at.rev;
    // The state held is the one the runtime had at the version held: never an older one over a newer.
    expect(now.feed, label).toBe(stateAt(version));
    expect(now.list, label).toBe(now.tab);
    expect(now.record.state.k, label).toBe(now.feed);
    if (isStamped(ID)) {
      const out = (OUT_OF_TURN as readonly string[]).includes(now.feed);
      expect(turnRuns(ID, { status: 'live' }), label).toBe(!out);
    }
  }
  const end = readings();
  expect(end.feed, label).toBe('ready');
  expect(end.list, label).toBe(CHAT_STATUS.none);
  expect(end.record, label).toMatchObject({ rev: 5, title: 'renamed' });
  expect(turnRuns(ID, { status: 'live' }), label).toBe(false);
}

describe('one state of a chat for the list, the tab and the feed', () => {
  beforeEach(() => {
    setScheduler('sync');
    machinesStore.set(M, {
      machine: { id: M },
      runtime: 'v2',
      epoch: EPOCH,
      features: [],
      limits: {},
    } as never);
  });

  it('the two channels in any interleaving show one state that never goes back', () => {
    const orders = interleavings(CONVS, CONV);
    expect(orders.length).toBe(35);
    orders.forEach((steps, n) => play(steps, `interleaving ${n}`));
  });

  it('the events in any order at all end in the state the runtime ended in', () => {
    const orders = permutations([...CONVS, ...CONV]);
    expect(orders.length).toBe(5040);
    orders.forEach((steps, n) => play(steps, `permutation ${n}`));
  });

  it('a stamp of a runtime that is gone says nothing of the one that answers now', () => {
    start();
    apply(fromConv(stamp('working', 90, 'e0')));
    expect(agentStateOf(ID)?.k).toBe('idle');
    apply(fromConv(stamp('working', 2)));
    expect(agentStateOf(ID)?.k).toBe('working');
  });

  it('the snapshot of the chat brings its state before the list has heard of it', () => {
    start();
    noteSnapshotStamp(ID, stamp('working', 2), true);
    expect(readings()).toMatchObject({
      list: CHAT_STATUS.thinking,
      tab: CHAT_STATUS.thinking,
      feed: 'working',
    });
    // The record waits at its own rev for the list's word of its other fields.
    expect(readings().record.rev).toBe(1);
    expect(isStamped(ID)).toBe(true);
  });

  it('a runtime that does not stamp the state: a live turn runs by its own word, as before', () => {
    start();
    expect(isStamped(ID)).toBe(false);
    // The older reading: the feed goes by the turn, whatever the list holds.
    expect(turnRuns(ID, { status: 'live' })).toBe(true);
    expect(turnRuns(ID, { status: 'done' })).toBe(false);
    apply(fromConvs({ type: 'conversation_state', id: ID, state: chat(2, 'working').state, rev: 2 }));
    apply(fromConvs({ type: 'conversation_state', id: ID, state: chat(5, 'ready').state, rev: 5 }));
    expect(turnRuns(ID, { status: 'live' })).toBe(true);

    // Once the chat's channel stamps the state, a turn left live is over when the state says so.
    apply(fromConv(stamp('ready', 5)));
    expect(isStamped(ID)).toBe(true);
    expect(turnRuns(ID, { status: 'live' })).toBe(false);

    // And back, when the machine answers with an older runtime again.
    noteSnapshotStamp(ID, undefined, false);
    expect(turnRuns(ID, { status: 'live' })).toBe(true);
  });
});
