import { afterEach, describe, expect, it } from 'vitest';
import {
  autoMarkOf,
  isHanded,
  ItemsBuilder,
  withOrigin,
  withRewinds,
  withSwitches,
} from '@/next/bindings/feed-items';
import type {
  FeedItem,
  ModelSwitchItem,
  OriginItem,
  RewoundItem,
  TurnItem,
} from '@/next/features/chat/types';
import {
  applyConvSnapshot,
  applyPermission,
  applyStep,
  applyTurn,
  dropConversationData,
  fedStore,
} from '@/next/stores/conversation';
import {
  applyMessage,
  applyPromptState,
  convMessagesStore,
  messagesStore,
  putLocal,
  waitsInQueue,
  type MessageView,
} from '@/next/stores/messages';
import { conversationsStore } from '@/next/stores/conversations';
import type { Conversation, Entry, Message, Permission, RewindMark, Turn } from '@/next/protocol/v2';

const CONV = 'tab-1';

afterEach(() => {
  conversationsStore.delete(CONV);
  dropConversationData(CONV);
  convMessagesStore.delete(CONV);
  messagesStore.delete('m-steer');
  messagesStore.delete('m-next');
});
const TURN = 'c:1#0';

const turn: Turn = {
  id: TURN,
  author: { provider: 'claude', role: null },
  startedAt: 1,
  endedAt: null,
  durationMs: null,
  toolCount: 0,
  stepCount: 0,
  status: 'live',
  failure: null,
  answerEntryId: null,
  files: [],
};

const readEntry: Entry = {
  t: 'user',
  id: 'c:2#0',
  turnId: TURN,
  at: 12,
  rev: 1,
  text: 'also check the logs',
  attachments: [],
  clientId: 'm-steer',
};

const prompt0: Extract<Entry, { t: 'user' }> = { ...readEntry, id: TURN, clientId: null };

const permission: Permission = {
  id: 'p:1',
  conversationId: CONV,
  turnId: TURN,
  kind: 'tool',
  askedAt: 5,
  tool: 'Bash',
  command: 'go test',
  cwd: null,
  planMd: null,
  options: [{ id: '1', label: 'Yes', scope: 'once' }],
  state: { k: 'open' },
};

const loaded = (messages: Message[]) =>
  applyConvSnapshot(
    CONV,
    'local',
    'replace',
    {
      turns: [turn],
      entries: [],
      beforeTurnId: null,
      now: null,
      queue: { conversationId: CONV, items: [], paused: null, rev: 1 },
      questions: [],
      permissions: [],
      background: [],
      messages,
    },
    1,
  );

const steered = (state: Message['state'], rev: number): Message => ({
  clientId: 'm-steer',
  conversationId: CONV,
  text: 'also check the logs',
  attachments: [],
  mode: 'steer',
  kind: 'prompt',
  createdAt: 10,
  attempts: 0,
  state,
  rev,
});

describe('a steered message', () => {
  it("stands in the stack «handed» while it waits in the agent's queue, and is the turn's once the agent read it", () => {
    loaded([]);
    applyMessage('local', steered({ k: 'in-input', wait: { slowAt: 13, deadlineAt: 1_000_000 } }, 1));
    const builder = new ItemsBuilder(CONV, 'local');
    const waiting = messagesStore.get('m-steer');
    expect(waiting && isHanded(waiting)).toBe(true);
    expect(waiting && waitsInQueue(waiting, 'working')).toBe(true);
    expect(builder.snapshot()?.some((item) => item.kind === 'outgoing')).toBe(false);

    // The agent read it: the transcript's entry stands in the turn, the stack lets go, the feed draws it once.
    applyStep(CONV, readEntry, null);
    applyPromptState(CONV, 'local', 'm-steer', { k: 'accepted', entryId: readEntry.id }, 2);
    applyPromptState(CONV, 'local', 'm-steer', { k: 'in-turn', turnId: TURN }, 3);
    builder.invalidate();
    const items = builder.snapshot();
    expect(items?.some((item) => item.kind === 'outgoing')).toBe(false);
    const inTurn = items?.find((item) => item.kind === 'turn');
    expect(inTurn?.kind === 'turn' ? inTurn.steps?.map((e) => e.t) : null).toEqual(['user']);
    expect(fedStore.get(CONV)?.has('m-steer')).toBe(true);
  });

  it("a normal message the runtime hands over to the agent's queue becomes «handed» by its upsert, not «accepted»", () => {
    loaded([]);
    const typed = steered({ k: 'in-input', wait: { slowAt: 13, deadlineAt: 20_000 } }, 1);
    applyMessage('local', { ...typed, mode: 'queue' });
    const before = messagesStore.get('m-steer');
    expect(before && isHanded(before)).toBe(false);
    // The agent had begun a turn of its own: the hook came into a running turn, so the message is in Claude's queue.
    applyMessage('local', {
      ...typed,
      state: { k: 'in-input', wait: { slowAt: 13, deadlineAt: 1_000_000 } },
      rev: 2,
    });
    const after = messagesStore.get('m-steer');
    expect(after?.mode).toBe('steer');
    expect(after && isHanded(after)).toBe(true);
    expect(after && waitsInQueue(after, 'working')).toBe(true);
  });

  it('is never in the feed and in the stack at once, even before the runtime says the agent has it', () => {
    loaded([]);
    applyMessage('local', steered({ k: 'in-input', wait: { slowAt: 13, deadlineAt: 1_000_000 } }, 1));
    applyStep(CONV, readEntry, null);
    const m = messagesStore.get('m-steer');
    const builder = new ItemsBuilder(CONV, 'local');
    expect(builder.snapshot()?.some((item) => item.kind === 'outgoing')).toBe(false);
    expect(m && fedStore.get(CONV)?.has(m.clientId)).toBe(true);
  });
});

describe('a message the hook accepted before the transcript shows it', () => {
  const queued = (): Message => ({ ...steered({ k: 'accepted', entryId: null }, 1), mode: 'queue' });

  it('stays in the feed as a pending bubble, once, and gives way to the user turn that carries its words', () => {
    loaded([]);
    applyMessage('local', queued());
    const builder = new ItemsBuilder(CONV, 'local');
    const pending = builder.snapshot()?.filter((item) => item.kind === 'outgoing');
    expect(pending).toHaveLength(1);
    const first = pending?.[0];
    expect(first?.kind === 'outgoing' && first.message.handed).toBe(true);

    // The agent took it: a turn whose prompt has the same words (no client id on the entry) replaces the bubble.
    applyTurn(CONV, { ...turn, id: 'c:9#0', startedAt: 21 });
    applyStep(CONV, { ...prompt0, id: 'c:9#0', turnId: 'c:9#0', at: 21, clientId: null }, null);
    builder.invalidate();
    const items = builder.snapshot();
    expect(items?.some((item) => item.kind === 'outgoing')).toBe(false);
    expect(
      items?.filter((item) => item.kind === 'turn' && item.prompt?.text === 'also check the logs'),
    ).toHaveLength(1);
  });

  it('is replaced by its entry by client id as well', () => {
    loaded([]);
    applyMessage('local', queued());
    applyTurn(CONV, { ...turn, id: 'c:9#0', startedAt: 21 });
    applyStep(CONV, { ...readEntry, id: 'c:9#0', turnId: 'c:9#0', at: 21, text: 'reworded' }, null);
    const items = new ItemsBuilder(CONV, 'local').snapshot();
    expect(items?.some((item) => item.kind === 'outgoing')).toBe(false);
  });
});

describe('a turn whose prompt the snapshot left out', () => {
  it('shows the message that opened it, with the pictures as they were sent', () => {
    const picture = { id: 'up_1', name: 'home.png', kind: 'image', mime: 'image/png' } as const;
    loaded([]);
    applyMessage('local', {
      ...steered({ k: 'done', turnId: TURN }, 4),
      clientId: 'm-next',
      text: 'describe the pictures',
      attachments: [picture],
      mode: 'queue',
    });
    const item = new ItemsBuilder(CONV, 'local').snapshot()?.find((x) => x.kind === 'turn');
    const prompt = item?.kind === 'turn' ? item.prompt : null;
    expect(prompt?.text).toBe('describe the pictures');
    expect(prompt?.attachments).toEqual([picture]);
    expect(prompt?.clientId).toBe('m-next');
  });
});

describe('order in the feed', () => {
  const prompt: Entry = {
    t: 'user',
    id: 'c:9#0',
    turnId: 'c:9#0',
    at: 21,
    rev: 1,
    text: 'next one',
    attachments: [],
    clientId: 'm-next',
  };
  const opened: Turn = { ...turn, id: 'c:9#0', startedAt: 21 };

  it('a turn that opens before its prompt entry arrives waits for it: the message stands above, never under the live indicator', () => {
    applyConvSnapshot(
      CONV,
      'local',
      'replace',
      {
        turns: [],
        entries: [],
        beforeTurnId: null,
        now: null,
        queue: { conversationId: CONV, items: [], paused: null, rev: 1 },
        questions: [],
        permissions: [],
        background: [],
        messages: [],
      },
      1,
    );
    applyMessage('local', {
      ...steered({ k: 'in-input', wait: { slowAt: 13, deadlineAt: 1_000_000 } }, 1),
      clientId: 'm-next',
      text: 'next one',
      mode: 'queue',
    });
    const builder = new ItemsBuilder(CONV, 'local');
    applyTurn(CONV, opened);
    builder.invalidate();
    // The turn is on the page, its prompt is not: the message is the only thing in the feed.
    expect(builder.snapshot()?.map((item) => item.kind)).toEqual(['outgoing']);

    applyStep(CONV, prompt, null);
    builder.invalidate();
    const items = builder.snapshot();
    expect(items?.map((item) => item.kind)).toEqual(['turn']);
    const only = items?.[0];
    expect(only?.kind === 'turn' ? only.prompt?.text : null).toBe('next one');
  });
});

describe('a refused message', () => {
  const refusedAt = (createdAt: number, text: string) =>
    putLocal({
      clientId: 'm-next',
      machineId: 'local',
      conversationId: CONV,
      text,
      attachments: [],
      mode: 'queue',
      kind: 'prompt',
      held: false,
      createdAt,
      stateAt: createdAt,
      attempts: 0,
      state: { k: 'failed', code: 'refused', retryable: false, at: createdAt },
      rev: 0,
      refused: 'agent-gone',
      wait: null,
      issue: null,
    });
  const kinds = (builder: ItemsBuilder) =>
    builder.snapshot()?.map((item) => (item.kind === 'turn' ? `turn@${item.turn.startedAt}` : item.kind));

  it('stands in its chronological place, before the turns that began after it, not under the live one', () => {
    loaded([]);
    applyTurn(CONV, { ...turn, startedAt: 50 });
    refusedAt(10, 'old words');
    const builder = new ItemsBuilder(CONV, 'local');
    expect(kinds(builder)).toEqual(['outgoing', 'turn@50']);
    // Written after the turn began: it is the latest thing and stands at the end.
    messagesStore.delete('m-next');
    refusedAt(60, 'new words');
    builder.invalidate();
    expect(kinds(builder)).toEqual(['turn@50', 'outgoing']);
  });

  it('is not drawn once a later turn opened with the same words', () => {
    loaded([]);
    applyTurn(CONV, { ...turn, startedAt: 50 });
    applyStep(CONV, { ...prompt0, text: 'same words' }, null);
    refusedAt(10, 'same words');
    expect(kinds(new ItemsBuilder(CONV, 'local'))).toEqual(['turn@50']);
    // Other words stay.
    messagesStore.delete('m-next');
    refusedAt(10, 'other words');
    expect(kinds(new ItemsBuilder(CONV, 'local'))).toEqual(['outgoing', 'turn@50']);
  });
});

describe('how long a message has waited', () => {
  it('counts from the state it is in, not from when it was written: a message that waited in the queue is not «not confirmed» the moment it is typed', () => {
    loaded([]);
    applyMessage('local', {
      ...steered({ k: 'queued', position: 1, reason: 'agent-busy', wait: null }, 1),
      clientId: 'm-next',
      mode: 'queue',
    });
    applyPromptState(CONV, 'local', 'm-next', { k: 'typing', wait: { slowAt: 1, deadlineAt: 2 } }, 2);
    const item = new ItemsBuilder(CONV, 'local').snapshot()?.find((x) => x.kind === 'outgoing');
    // createdAt is 10 (long ago); the state began now.
    expect(item?.kind === 'outgoing' ? item.message.stateSince : 0).toBeGreaterThan(1_000_000_000_000);
  });
});

describe('where a message stands on its way', () => {
  const view = (patch: Partial<MessageView>): MessageView => ({
    clientId: 'm-1',
    machineId: 'local',
    conversationId: CONV,
    text: 'hi',
    attachments: [],
    mode: 'queue',
    kind: 'prompt',
    held: false,
    createdAt: 1,
    stateAt: 1,
    attempts: 0,
    state: { k: 'submitting' },
    rev: 0,
    refused: null,
    wait: null,
    issue: null,
    ...patch,
  });

  it('a message written to a busy agent is in the stack from the first moment to the moment the agent has it', () => {
    const held = { held: true };
    expect(waitsInQueue(view(held), 'working')).toBe(true);
    expect(
      waitsInQueue(
        view({ ...held, rev: 1, state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null } }),
        'working',
      ),
    ).toBe(true);
    expect(
      waitsInQueue(
        view({ ...held, rev: 2, state: { k: 'typing', wait: { slowAt: 1, deadlineAt: 2 } } }),
        'idle',
      ),
    ).toBe(true);
    expect(waitsInQueue(view({ ...held, rev: 3, state: { k: 'accepted', entryId: null } }), 'working')).toBe(
      false,
    );
    expect(waitsInQueue(view({ ...held, rev: 4, state: { k: 'in-turn', turnId: TURN } }), 'working')).toBe(
      false,
    );
  });

  it('a message written to an agent that takes it is in the feed on its way, never in the stack', () => {
    expect(waitsInQueue(view({}), 'idle')).toBe(false);
    expect(
      waitsInQueue(
        view({ rev: 1, state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null } }),
        'idle',
      ),
    ).toBe(false);
    expect(
      waitsInQueue(view({ rev: 2, state: { k: 'typing', wait: { slowAt: 1, deadlineAt: 2 } } }), 'idle'),
    ).toBe(false);
  });

  it('a message that failed stands where it was, with its retry', () => {
    const failed = { k: 'failed', code: 'refused', retryable: true, at: 5 } as const;
    expect(waitsInQueue(view({ held: true, state: failed }), 'working')).toBe(true);
    expect(waitsInQueue(view({ state: failed }), 'working')).toBe(false);
  });
});

describe('feed items', () => {
  it('an open permission without a gate entry stands at the end of its turn', () => {
    applyConvSnapshot(
      CONV,
      'local',
      'replace',
      {
        turns: [turn],
        entries: [],
        beforeTurnId: null,
        now: null,
        queue: { conversationId: CONV, items: [], paused: null, rev: 1 },
        questions: [],
        permissions: [],
        background: [],
        messages: [],
      },
      1,
    );
    applyTurn(CONV, turn);
    const builder = new ItemsBuilder(CONV, 'local');
    const before = builder.snapshot();
    expect(before?.[0]).toMatchObject({ kind: 'turn' });
    applyPermission(CONV, permission);
    builder.invalidate();
    const after = builder.snapshot();
    const item = after?.[0];
    expect(item && item.kind === 'turn' ? item.steps?.map((e) => e.t) : null).toEqual(['gate']);
  });
});

describe('a turn a subagent woke', () => {
  it('has the agent as its woke entry, not as a step or a prompt', () => {
    applyConvSnapshot(
      CONV,
      'local',
      'replace',
      {
        turns: [{ ...turn, status: 'done', stepCount: 1 }],
        entries: [
          {
            id: TURN,
            turnId: TURN,
            at: 1,
            rev: 1,
            t: 'subagent',
            agentId: 'a77',
            agentType: 'ci',
            description: 'Speed up CI',
            status: 'done',
            model: null,
            durationMs: null,
            output: { text: 'Cut 12 minutes.', truncated: null },
          },
        ],
        beforeTurnId: null,
        now: null,
        queue: { conversationId: CONV, items: [], paused: null, rev: 1 },
        questions: [],
        permissions: [],
        background: [],
        messages: [],
      },
      1,
    );
    const item = new ItemsBuilder(CONV, 'local').snapshot()?.[0];
    expect(item).toMatchObject({ kind: 'turn', prompt: null, woke: { agentId: 'a77' }, steps: [] });
  });
});

describe('applied changes of model among the turns', () => {
  const turnAt = (id: string, startedAt: number): TurnItem =>
    ({ kind: 'turn', key: id, turn: { ...turn, id, startedAt } }) as TurnItem;
  const itemOf = (sw: { at: number }): ModelSwitchItem => ({
    kind: 'switch',
    key: `switch:${sw.at}`,
    at: sw.at,
    from: null,
    to: { id: 'sonnet', effort: null },
  });
  const log = (...ats: number[]) => ats.map((at) => ({ at, from: null, to: { id: 'sonnet', effort: null } }));
  const keys = (items: FeedItem[]) => items.map((x) => x.key);

  it('stands before the first turn that started after it', () => {
    const turns = [turnAt('a', 10), turnAt('b', 30)];
    expect(keys(withSwitches(turns, log(20), false, itemOf))).toEqual(['a', 'switch:20', 'b']);
  });

  it('stands at the end when no turn started after it', () => {
    const turns = [turnAt('a', 10)];
    expect(keys(withSwitches(turns, log(20), false, itemOf))).toEqual(['a', 'switch:20']);
  });

  it('keeps several in the order they happened', () => {
    const turns = [turnAt('a', 10), turnAt('b', 30)];
    expect(keys(withSwitches(turns, log(25, 15), false, itemOf))).toEqual([
      'a',
      'switch:15',
      'switch:25',
      'b',
    ]);
  });

  it('waits for older turns that are not loaded yet instead of standing above all of them', () => {
    const turns = [turnAt('b', 30)];
    expect(keys(withSwitches(turns, log(5), true, itemOf))).toEqual(['b']);
    expect(keys(withSwitches(turns, log(5), false, itemOf))).toEqual(['switch:5', 'b']);
  });

  it('is the same list when there is nothing to add', () => {
    const turns = [turnAt('a', 10)];
    expect(keys(withSwitches(turns, [], false, itemOf))).toEqual(['a']);
  });
});

describe('rewinds among the turns', () => {
  const turnAt = (id: string, startedAt: number): TurnItem =>
    ({ kind: 'turn', key: id, turn: { ...turn, id, startedAt } }) as TurnItem;
  const mark = (afterTurnId: string | null, at: number): RewindMark => ({
    at,
    afterTurnId,
    turns: 2,
    files: false,
    undoUntil: at + 15_000,
  });
  const itemOf = (m: RewindMark, n: number): RewoundItem => ({
    kind: 'rewound',
    key: `rewound:${m.at}`,
    n,
    at: m.at,
    turns: m.turns,
    files: m.files,
    undoUntil: null,
  });
  const keys = (items: FeedItem[]) => items.map((x) => x.key);

  it('folds into one line right after the last turn it kept', () => {
    const turns = [turnAt('a', 10), turnAt('b', 30)];
    expect(keys(withRewinds(turns, [mark('a', 20)], false, itemOf))).toEqual(['a', 'rewound:20', 'b']);
  });

  it('stands above the first turn when it kept none, once the feed has no older turns', () => {
    const turns = [turnAt('b', 30)];
    expect(keys(withRewinds(turns, [mark(null, 20)], false, itemOf))).toEqual(['rewound:20', 'b']);
    expect(keys(withRewinds(turns, [mark(null, 20)], true, itemOf))).toEqual(['b']);
  });

  it('is not drawn after another turn of the same id that started after it', () => {
    // A Codex turn is named by where it is in the transcript: the turn that took its place has its id.
    const turns = [turnAt('a', 10), turnAt('b', 50)];
    expect(keys(withRewinds(turns, [mark('b', 40)], false, itemOf))).toEqual(['a', 'b']);
  });

  it('is not drawn when its turn is not on screen (a later rewind left it behind)', () => {
    const turns = [turnAt('a', 10), turnAt('c', 60)];
    expect(keys(withRewinds(turns, [mark('b', 40), mark('a', 50)], false, itemOf))).toEqual([
      'a',
      'rewound:50',
      'c',
    ]);
  });
});

describe('where the chat came from', () => {
  const turnAt = (id: string): TurnItem => ({ kind: 'turn', key: id, turn: { ...turn, id } }) as TurnItem;
  const origin: OriginItem = {
    kind: 'origin',
    key: 'origin',
    at: 5,
    fork: true,
    fromId: 'tab-a',
    title: 'Source',
    from: null,
    to: null,
  };
  const keys = (items: readonly FeedItem[]) => items.map((x) => x.key);
  const turns = [turnAt('a'), turnAt('b'), turnAt('c')];

  it('stands after the turn a fork branched at', () => {
    expect(keys(withOrigin(turns, origin, 'b', false))).toEqual(['a', 'b', 'origin', 'c']);
  });

  it('stands above everything when that turn is not in the feed, once the feed reaches its start', () => {
    expect(keys(withOrigin(turns, origin, 'x:1#0', false))).toEqual(['origin', 'a', 'b', 'c']);
    expect(keys(withOrigin(turns, origin, null, true))).toEqual(['a', 'b', 'c']);
  });

  it('leaves the items as they are for a chat started anew', () => {
    expect(withOrigin(turns, null, null, false)).toBe(turns);
  });
});

describe('where the runtime went on by itself', () => {
  const ownEntry: Extract<Entry, { t: 'user' }> = { ...prompt0, clientId: 'm-auto' };
  const wrote = (patch: Partial<Message>): Message => ({
    clientId: 'm-auto',
    conversationId: CONV,
    text: 'Continue from where you stopped',
    attachments: [],
    mode: 'queue',
    kind: 'prompt',
    createdAt: 10,
    attempts: 0,
    state: { k: 'in-turn', turnId: TURN },
    rev: 1,
    ...patch,
  });
  const record = (autoContinues: Conversation['autoContinues']) =>
    conversationsStore.set(CONV, {
      id: CONV,
      state: { k: 'idle' },
      autoContinues,
    } as unknown as Conversation);
  const withPrompt = (entry: Extract<Entry, { t: 'user' }>) => {
    loaded([]);
    applyStep(CONV, entry, null);
  };
  const turnOfFeed = (builder: ItemsBuilder) => builder.snapshot()?.find((x) => x.kind === 'turn');

  afterEach(() => messagesStore.delete('m-auto'));

  const go = (why: 'limit' | 'restart', own?: true) =>
    new Map([['m-1', { at: 1, why, clientId: 'm-1', ...(own && { own }) }]]);

  it('marks a message the runtime wrote, by its flag and not by its words', () => {
    expect(autoMarkOf('m-1', { auto: 'limit' }, new Map())).toEqual({ why: 'limit', own: true });
    expect(autoMarkOf('m-1', { auto: 'restart' }, go('limit'))).toEqual({ why: 'restart', own: true });
    expect(autoMarkOf('m-1', {}, new Map())).toBeUndefined();
    expect(autoMarkOf(null, undefined, go('limit'))).toBeUndefined();
  });

  it("marks the runtime's own message by the list once the message itself is gone from the page", () => {
    expect(autoMarkOf('m-1', undefined, go('restart', true))).toEqual({ why: 'restart', own: true });
  });

  it("marks the person's own queued message that the runtime sent, as theirs", () => {
    expect(autoMarkOf('m-1', {}, go('limit'))).toEqual({ why: 'limit', own: false });
  });

  it('shares the marks, so a row whose message is the same does not draw again', () => {
    const goes = go('limit');
    expect(autoMarkOf('m-1', {}, goes)).toBe(autoMarkOf('m-1', undefined, goes));
  });

  it("puts the line on the turn the runtime's own message opened, in place of the bubble", () => {
    withPrompt(ownEntry);
    applyMessage('local', wrote({ auto: 'limit' }));
    record([{ at: 12, why: 'limit', clientId: 'm-auto' }]);
    const item = turnOfFeed(new ItemsBuilder(CONV, 'local'));
    expect(item?.kind === 'turn' ? item.auto : null).toEqual({ why: 'limit', own: true });
  });

  it("puts the line above the bubble of the person's message the runtime sent", () => {
    withPrompt(ownEntry);
    applyMessage('local', wrote({}));
    record([{ at: 12, why: 'restart', clientId: 'm-auto' }]);
    const item = turnOfFeed(new ItemsBuilder(CONV, 'local'));
    expect(item?.kind === 'turn' ? item.auto : null).toEqual({ why: 'restart', own: false });
  });

  it('leaves a turn the runtime did not start by itself without a line', () => {
    withPrompt({ ...ownEntry, clientId: 'm-other' });
    record([{ at: 12, why: 'limit', clientId: 'm-auto' }]);
    const item = turnOfFeed(new ItemsBuilder(CONV, 'local'));
    expect(item?.kind === 'turn' ? item.auto : 'x').toBeUndefined();
  });

  it("draws the runtime's own message on its way as a line, and not in the queue strip", () => {
    loaded([]);
    applyMessage(
      'local',
      wrote({ auto: 'limit', state: { k: 'queued', position: 1, reason: 'paused', wait: null } }),
    );
    const own = messagesStore.get('m-auto');
    expect(own && waitsInQueue(own, 'idle')).toBe(false);
    const out = new ItemsBuilder(CONV, 'local').snapshot()?.find((x) => x.kind === 'outgoing');
    expect(out?.kind === 'outgoing' ? out.auto : null).toEqual({ why: 'limit', own: true });
  });

  it("does not offer a retry of the runtime's own message that failed", () => {
    loaded([]);
    applyMessage(
      'local',
      wrote({ auto: 'restart', state: { k: 'failed', code: 'type-failed', retryable: true, at: 11 } }),
    );
    expect(new ItemsBuilder(CONV, 'local').snapshot()?.some((x) => x.kind === 'outgoing')).toBe(false);
  });
});
