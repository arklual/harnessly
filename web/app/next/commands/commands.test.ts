import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { machineNow } from '@/next/core/clock';
import { deviceId } from '@/next/core/device';
import { storeSink } from '@/next/core/realtime/apply';
import { hub } from '@/next/core/realtime/hub';
import { fixture } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import {
  answerQuestion,
  cancelMessage,
  createConversation,
  moveQueued,
  promoteMessage,
  renameConversation,
  retryCommand,
  resendRefused,
  retryMessage,
  runCommand,
  sendMessage,
  flushOutbox,
} from '@/next/commands';
import { answeredElsewhere, feedStore, gatesStore, openGates } from '@/next/stores/conversation';
import {
  applyMessage,
  canMoveLocally,
  convMessagesStore,
  messagePhase,
  messagesStore,
} from '@/next/stores/messages';
import { opsStore } from '@/next/stores/ops';
import type {
  ClientFrame,
  ConvEvent,
  ConvChannel,
  ConvId,
  ConvSnapshot,
  Conversation,
  Message,
  MessageState,
  Question,
} from '@/next/protocol/v2';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;

const stateOf = (name: string): MessageState => fixture<{ e: { state: MessageState } }>(name).e.state;
const msg = (id: string) => messagesStore.get(id)!;
/** Runs the fake network (latency on the fake timers) until the op ends. */
const finish = async (h: { done: Promise<unknown> }) => {
  await settle();
  return h.done as Promise<Awaited<ReturnType<typeof runCommand>['done']>>;
};

beforeEach(useFakeTime);
afterEach(() => {
  hub.stop();
  vi.useRealTimers();
});

async function openChat() {
  const boot = await bootFake();
  const release = hub.openConversation(boot.m, CONV);
  await settle();
  return { ...boot, release };
}

describe('a message from draft to its end', () => {
  it('follows the runtime state by state; an older rev changes nothing', async () => {
    const { rt, m } = await openChat();
    expect(canMoveLocally({ k: 'draft' }, { k: 'submitting' })).toBe(true);
    expect(canMoveLocally({ k: 'draft' }, { k: 'done', turnId: 'x:1#0' })).toBe(false);

    const id = sendMessage(m, { conversationId: CONV, text: 'Проверь на Safari' });
    // In the same frame: submitting, and the op is pending.
    expect(msg(id).state).toEqual({ k: 'submitting' });
    expect(opsStore.get(id)).toMatchObject({ k: 'pending', op: 'send' });
    await settle();
    expect(opsStore.get(id)?.k).toBe('done');
    expect(msg(id)).toMatchObject({ rev: 1, state: { k: 'queued', reason: 'agent-busy' } });

    let rev = 1;
    for (const name of ['typing', 'in-input', 'accepted', 'in-turn', 'done']) {
      rev += 1;
      rt.emit(CH, { type: 'prompt_state', clientId: id, state: stateOf(`ev.prompt_state-${name}`), rev });
      expect(msg(id).state.k).toBe(name);
    }
    rt.emit(CH, { type: 'prompt_state', clientId: id, state: stateOf('ev.prompt_state-typing'), rev: 3 });
    expect(msg(id).state.k).toBe('done');
  });

  it('ends failed and retries through the runtime, or cancelled', async () => {
    const { rt, m } = await openChat();
    const id = sendMessage(m, { conversationId: CONV, text: 'a' });
    await settle();
    rt.emit(CH, { type: 'prompt_state', clientId: id, state: stateOf('ev.prompt_state-failed'), rev: 2 });
    expect(msg(id).state).toMatchObject({ k: 'failed', code: 'expired', retryable: true });
    rt.handlers.set('retry', (_, r) => ({
      r: 'queued',
      ch: CH,
      seq: r.emit(CH, { type: 'prompt_state', clientId: id, state: stateOf('ev.prompt_state'), rev: 3 }),
    }));
    const h = retryMessage(id)!;
    expect((await finish(h)).k).toBe('done');
    expect(msg(id).state.k).toBe('queued');

    rt.handlers.set('cancel', (_, r) => ({
      r: 'accepted',
      ch: CH,
      seq: r.emit(CH, {
        type: 'prompt_state',
        clientId: id,
        state: stateOf('ev.prompt_state-cancelled'),
        rev: 4,
      }),
    }));
    // Pessimistic: nothing changes before the runtime says so.
    const c = cancelMessage(id)!;
    expect(msg(id).state.k).toBe('queued');
    expect((await finish(c)).k).toBe('done');
    expect(msg(id).state).toEqual({ k: 'cancelled', by: 'user' });
  });

  it('shows «дольше обычного» at slowMs and ends at cmdTimeoutMs on the clock, then retries with the same clientId', async () => {
    const { rt, m } = await openChat();
    rt.hang = true;
    const id = sendMessage(m, { conversationId: CONV, text: 'Долго' });
    expect(messagePhase(msg(id), machineNow(m))).toBe('normal');
    await vi.advanceTimersByTimeAsync(3000);
    expect(messagePhase(msg(id), machineNow(m))).toBe('slow');
    expect(opsStore.get(id)).toMatchObject({ k: 'pending', slow: true });
    await vi.advanceTimersByTimeAsync(9000);
    expect(opsStore.get(id)).toMatchObject({ k: 'failed', retryable: true, failure: { code: 'timeout' } });
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, issue: 'timeout' });
    expect(messagePhase(msg(id), machineNow(m))).toBe('overdue');

    rt.hang = false;
    retryMessage(id);
    expect(messagePhase(msg(id), machineNow(m))).toBe('normal');
    await settle();
    expect(msg(id).state.k).toBe('queued');
    const sends = rt.commands.filter((c) => c.op === 'send');
    expect(sends.map((c) => c.body.opId)).toEqual([id]);
  });

  it('a wait of the runtime turns slow and overdue by the machine clock', async () => {
    const { rt, m } = await openChat();
    const id = sendMessage(m, { conversationId: CONV, text: 'в меню' });
    await settle();
    // queued{blocked-menu}: slowAt is 3 s after the welcome's serverTime, deadlineAt 15 min.
    rt.emit(CH, { type: 'prompt_state', clientId: id, state: stateOf('ev.prompt_state'), rev: 2 });
    expect(messagePhase(msg(id), machineNow(m))).toBe('normal');
    await vi.advanceTimersByTimeAsync(3000);
    expect(messagePhase(msg(id), machineNow(m))).toBe('slow');
  });

  it('the road failing is never a refusal: 502, «machine offline» and the rest wait for the machine and go by themselves', async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 1;
    let notThere = 2;
    rt.handlers.set('send', (cmd, r) =>
      notThere-- > 0
        ? { r: 'refused', code: 'machine-offline' }
        : r.defaultSend(cmd as Extract<typeof cmd, { op: 'send' }>),
    );
    const id = sendMessage(m, { conversationId: CONV, text: 'Во время выката' });
    await settle();
    // The gateway's 502 page: waiting, quiet, no red.
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, issue: 'http', refused: null });
    await vi.advanceTimersByTimeAsync(1100);
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, issue: 'machine-offline', refused: null });
    await vi.advanceTimersByTimeAsync(2100);
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, issue: 'machine-offline', refused: null });
    // The machine is back: the next step sends it, and the runtime's own state replaces the page's wait.
    await vi.advanceTimersByTimeAsync(4100);
    expect(msg(id)).toMatchObject({ rev: 1, state: { k: 'queued' }, issue: null, refused: null });
    // The same clientId every time, and nothing sends it after the runtime has it.
    expect(new Set(rt.commands.filter((c) => c.op === 'send').map((c) => c.body.opId))).toEqual(
      new Set([id]),
    );
    const sent = rt.commands.length;
    await vi.advanceTimersByTimeAsync(120_000);
    expect(rt.commands.length).toBe(sent);
  });

  it('a live socket sends a waiting message at once, not at the next step of the timer', async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 1;
    const id = sendMessage(m, { conversationId: CONV, text: 'Связь пропала' });
    await settle();
    expect(msg(id).state.k).toBe('submitting');
    // The socket is live again (startOutbox calls this): the message goes before the timer's first step.
    flushOutbox(m);
    await vi.advanceTimersByTimeAsync(50);
    expect(msg(id)).toMatchObject({ rev: 1, state: { k: 'queued' } });
  });

  it("Delete on a waiting message is the page's own: it goes at once, offline too, and nothing sends it later", async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 99;
    const id = sendMessage(m, { conversationId: CONV, text: 'Не нужно' });
    await settle();
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, issue: 'http' });
    cancelMessage(id);
    expect(msg(id).state).toEqual({ k: 'cancelled', by: 'user' });
    rt.gatewayErrors = 0;
    await vi.advanceTimersByTimeAsync(60_000);
    expect(rt.commands.filter((c) => c.op === 'send')).toEqual([]);
    expect(msg(id).state.k).toBe('cancelled');
  });

  it('a refusal ends failed{refused} with its code; only retryable codes offer a retry', async () => {
    const { rt, m } = await openChat();
    rt.handlers.set('send', () => ({ r: 'refused', code: 'queue-full' }));
    const id = sendMessage(m, { conversationId: CONV, text: 'x' });
    await settle();
    expect(msg(id)).toMatchObject({
      state: { k: 'failed', code: 'refused', retryable: false },
      refused: 'queue-full',
    });
    expect(opsStore.get(id)).toMatchObject({ k: 'failed', retryable: false });
    expect(retryMessage(id)).toBeNull();
    expect(msg(id).state.k).toBe('failed');
    cancelMessage(id);
    expect(msg(id).state).toEqual({ k: 'cancelled', by: 'user' });
  });
});

describe('a refusal whose cause may have passed', () => {
  it('can be sent again as a new message with the same words; a refusal on the merits cannot', async () => {
    const { rt, m } = await openChat();
    rt.handlers.set('send', () => ({ r: 'refused', code: 'agent-gone' }));
    const id = sendMessage(m, { conversationId: CONV, text: 'ещё раз' });
    await settle();
    // agent-gone is not on the road: the person sees the refusal.
    expect(msg(id)).toMatchObject({ state: { k: 'failed', code: 'refused' }, refused: 'agent-gone' });
    rt.handlers.delete('send');
    const next = resendRefused(id);
    expect(next).not.toBeNull();
    expect(next).not.toBe(id);
    expect(msg(id).state).toEqual({ k: 'cancelled', by: 'user' });
    expect(msg(next!)).toMatchObject({ text: 'ещё раз', conversationId: CONV });
    await settle();
    expect(msg(next!).state.k).not.toBe('failed');

    rt.handlers.set('send', () => ({ r: 'refused', code: 'text-too-long' }));
    const long = sendMessage(m, { conversationId: CONV, text: 'длинно' });
    await settle();
    expect(msg(long).refused).toBe('text-too-long');
    expect(resendRefused(long)).toBeNull();
    expect(msg(long).state.k).toBe('failed');
  });
});

describe('commands', () => {
  it('retry after a lost answer reuses the opId; the runtime does it once', async () => {
    const { rt, m } = await openChat();
    rt.loseAnswers = 1;
    const id = sendMessage(m, { conversationId: CONV, text: 'один раз' });
    await settle();
    expect(opsStore.get(id)).toMatchObject({ k: 'failed', failure: { code: 'network' }, retryable: true });
    // The runtime has it already: the socket shows it while the page still thinks it is unanswered.
    expect(msg(id).rev).toBe(1);
    const again = retryCommand(id)!;
    expect((await finish(again)).k).toBe('done');
    expect(rt.commands.filter((c) => c.body.opId === id)).toHaveLength(2);
    const upserts = rt.ring(CH).events.filter((e) => e.e.type === 'message_upsert');
    expect(upserts).toHaveLength(1);
  });

  it('the same opId with another body is refused{conflict}', async () => {
    const { m } = await openChat();
    const a = runCommand(m, 'conversation.rename', { id: CONV, title: 'A' }, 'op-r1');
    expect((await finish(a)).k).toBe('done');
    const b = runCommand(m, 'conversation.rename', { id: CONV, title: 'B' }, 'op-r1');
    expect(await finish(b)).toMatchObject({
      k: 'failed',
      failure: { r: 'refused', code: 'conflict' },
      retryable: false,
    });
  });

  it('is done only when the socket has shown the result seq', async () => {
    const { rt, m } = await openChat();
    let seq = 0;
    rt.handlers.set('conversation.rename', () => ({
      r: 'accepted',
      ch: 'convs',
      seq: (seq = rt.ring('convs').seq + 1),
    }));
    const h = renameConversation(m, CONV, 'Новое');
    await settle();
    expect(opsStore.get(h.opId)).toMatchObject({ k: 'pending', stage: 'effect' });
    const conv = fixture<{ e: { conv: Conversation } }>('ev.conversation_upsert').e.conv;
    rt.emit('convs', {
      type: 'conversation_upsert',
      conv: { ...conv, id: CONV, title: 'Новое', rev: 99 },
    });
    expect(rt.ring('convs').seq).toBe(seq);
    await settle();
    expect(opsStore.get(h.opId)?.k).toBe('done');
  });

  it('create with a first message subscribes the new chat from afterSeq 0 and keeps the message', async () => {
    const { rt, m } = await bootFake();
    const h = createConversation(m, {
      where: { projectId: 'p7k2m9x4q1aa' },
      provider: 'claude',
      model: { id: 'claude-opus-5-5', effort: 'medium' },
      first: { text: 'Начни с §9' },
    });
    expect(msg(h.firstClientId!).state).toEqual({ k: 'submitting' });
    const id = (await settle(), await h.conversation);
    await settle();
    expect(id).toBe('tab-N3wConv01');
    const sock = rt.opened[rt.opened.length - 1];
    const sub = sock.sent.find(
      (f): f is Extract<ClientFrame, { f: 'sub' }> => f.f === 'sub' && f.ch === `conv:${id}`,
    );
    expect(sub).toEqual({ f: 'sub', ch: `conv:${id}`, epoch: rt.epoch, afterSeq: 0 });
    // no snapshot comes for a channel replayed from its first event: the feed is empty-and-loaded, not a skeleton
    expect(feedStore.get(id!)).toMatchObject({ loaded: true, source: 'live', rows: [] });
    expect(msg(h.firstClientId!)).toMatchObject({
      conversationId: id,
      rev: 1,
      state: { k: 'queued', reason: 'launching' },
    });
  });
});

describe('gates', () => {
  it('a question answered from another device closes here with «ответили с другого устройства»', async () => {
    const { rt } = await openChat();
    const q = fixture<{ e: { question: Question } }>('ev.question_asked').e.question;
    rt.emit(CH, { type: 'question_asked', question: q });
    expect(openGates(gatesStore.get(CONV)!).questions.map((x) => x.id)).toEqual([q.id]);
    const answered = fixture<{ e: ConvEvent }>('ev.gate_updated-answered').e;
    rt.emit(CH, answered);
    const g = gatesStore.get(CONV)!;
    expect(openGates(g).questions).toEqual([]);
    expect(answeredElsewhere(g.questions.find((x) => x.id === q.id)!.state, deviceId())).toBe(true);
  });

  it('answered here: answering, then answered by this device', async () => {
    const { rt, m } = await openChat();
    const q = fixture<{ e: { question: Question } }>('ev.question_asked').e.question;
    rt.emit(CH, { type: 'question_asked', question: q });
    rt.handlers.set('answer', (cmd, r) => {
      r.emit(CH, {
        type: 'gate_updated',
        id: q.id,
        state: {
          k: 'answering',
          opId: cmd.body.opId,
          wait: { slowAt: 1792656003000, deadlineAt: 1792656010000 },
        },
      });
      const seq = r.emit(CH, {
        type: 'gate_updated',
        id: q.id,
        state: { k: 'answered', at: 1792656001000, byDevice: deviceId() },
      });
      return { r: 'accepted', ch: CH, seq };
    });
    const h = answerQuestion(m, CONV, q.id, [
      { i: 0, choices: [0] },
      { i: 1, choices: [], text: 'и HTTP' },
    ]);
    expect(opsStore.get(h.opId)?.k).toBe('pending');
    expect((await finish(h)).k).toBe('done');
    const st = gatesStore.get(CONV)!.questions[0].state;
    expect(st.k).toBe('answered');
    expect(answeredElsewhere(st, deviceId())).toBe(false);
  });
});

describe('a stale queued row of a message that was delivered', () => {
  // As the four rows on prod: kept in the browser as queued{agent-busy} with pictures, answered meanwhile.
  const record = (clientId: string): Message => ({
    clientId,
    conversationId: CONV,
    text: 'Смотри, агент мне отправил скриншот, а он у меня вообще никак не мапится в чате.',
    attachments: [{ id: 'up_5GURGK5YLFFX2MQ4', name: 'CleanShot.png', kind: 'image', mime: 'image/png' }],
    mode: 'queue',
    kind: 'prompt',
    createdAt: 1792894862307,
    attempts: 0,
    state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null },
    rev: 4,
  });
  const stale = (m: Parameters<typeof applyMessage>[0], clientId: string) =>
    applyMessage(m, record(clientId));

  it('Delete: the runtime answers with the message as it is (done) and the row leaves, no «не получилось»', async () => {
    const { rt, m } = await openChat();
    const id = '01a170dc-82c8-7f2c-8397-e070a29956f4';
    stale(m, id);
    rt.handlers.set('cancel', (_, r) => ({
      r: 'accepted',
      ch: CH,
      seq: r.emit(CH, {
        type: 'message_upsert',
        message: {
          ...record(id),
          state: { k: 'done', turnId: 'c:6ae611ed-6814-4bd2-86fe-7d304937df53#0' },
          rev: 8,
        },
      }),
    }));
    const st = await finish(cancelMessage(id)!);
    expect(st.k).toBe('done');
    expect(msg(id).state.k).toBe('done');
  });

  it('Delete, «Сразу» and a move of a message the runtime let go of: the page forgets its copy and the op is done', async () => {
    const { rt, m } = await openChat();
    const ids = [
      '01a170d2-4226-7110-a953-4ec030b1c0cc',
      '01a170db-b45b-769c-a4ab-0236cf95cf75',
      '01a170dc-1403-7834-b13f-05cf994a9033',
    ];
    for (const id of ids) stale(m, id);
    for (const op of ['cancel', 'promote', 'queue.move'] as const)
      rt.handlers.set(op, () => ({ r: 'refused', code: 'not-found' }));
    const results = [
      await finish(cancelMessage(ids[0]!)!),
      await finish(promoteMessage(ids[1]!, 'now')!),
      await finish(moveQueued(m, CONV, ids[2]!, null)),
    ];
    expect(results.map((r) => r.k)).toEqual(['done', 'done', 'done']);
    for (const id of ids) {
      expect(messagesStore.get(id)).toBeUndefined();
      expect(convMessagesStore.get(CONV) ?? []).not.toContain(id);
    }
  });

  it('a refusal on the merits still fails: only not-found means the message is gone', async () => {
    const { rt, m } = await openChat();
    const id = '01a170dc-82c8-7f2c-8397-e070a29956f4';
    stale(m, id);
    rt.handlers.set('cancel', () => ({ r: 'refused', code: 'too-late' }));
    const st = await finish(cancelMessage(id)!);
    expect(st.k).toBe('failed');
    expect(msg(id).state.k).toBe('queued');
  });
});

describe('the snapshot is the runtime word on what is pending', () => {
  it('a message the runtime never had survives a snapshot that does not list it and goes once, by its clientId', async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 1;
    const id = sendMessage(m, { conversationId: CONV, text: 'Написано без связи' });
    await settle();
    expect(msg(id)).toMatchObject({ rev: 0, state: { k: 'submitting' } });
    // A resync that does not know it (the runtime never had it): the page keeps its own copy.
    const conv = fixture<{ ch: ConvChannel; data: ConvSnapshot }>('snapshot.conv');
    storeSink.snapshot(m, { ch: CH, mode: 'replace', data: { ...conv.data, messages: [] } } as never);
    expect(msg(id)).toMatchObject({ rev: 0, state: { k: 'submitting' } });
    flushOutbox(m);
    await vi.advanceTimersByTimeAsync(50);
    expect(msg(id)).toMatchObject({ rev: 1, state: { k: 'queued' } });
    await vi.advanceTimersByTimeAsync(120_000);
    const sends = rt.commands.filter((c) => c.op === 'send');
    expect(new Set(sends.map((c) => c.body.opId))).toEqual(new Set([id]));
  });
});
