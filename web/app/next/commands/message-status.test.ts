import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { machineNow } from '@/next/core/clock';
import { hub } from '@/next/core/realtime/hub';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { flushOutbox, retryMessage, sendMessage, startOutbox } from '@/next/commands';
import { STATUS_EVERY_MS } from '@/next/commands/message-status';
import { applyMessage, messagesStore, putLocal, waitsInQueue } from '@/next/stores/messages';
import type { ConvId, Message } from '@/next/protocol/v2';
import type { FakeRuntime } from '@/next/core/testing/fake-runtime';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const msg = (id: string) => messagesStore.get(id)!;

let stop: (() => void) | null = null;
beforeEach(useFakeTime);
afterEach(() => {
  stop?.();
  stop = null;
  hub.stop();
  vi.useRealTimers();
});

async function openChat() {
  const boot = await bootFake();
  hub.openConversation(boot.m, CONV);
  await settle();
  stop = startOutbox();
  return boot;
}

/** The asks about this test's messages: the fixture's own queued ones are asked about too. */
const ours = (asks: string[][]) =>
  asks.map((ids) => ids.filter((id) => id.startsWith('m-'))).filter((ids) => ids.length > 0);

const record = (clientId: string, state: Message['state'], rev: number): Message => ({
  clientId,
  conversationId: CONV,
  text: 'как дела',
  attachments: [],
  mode: 'queue',
  kind: 'prompt',
  createdAt: 1792656000000,
  attempts: 0,
  state,
  rev,
});

/**
 * The runtime answers a send from its memory of the opId — the message's events went out long ago, this page never
 * saw them — so the answer comes with no event of the message: the channel's seq of some other event.
 */
function answerFromMemory(rt: FakeRuntime, keep: (clientId: string) => Message | null): void {
  rt.handlers.set('send', (cmd) => {
    if (cmd.op !== 'send') throw new Error(cmd.op);
    const data = rt.convs.get(CONV)!;
    const kept = keep(cmd.body.clientId);
    if (kept) data.messages = [...data.messages, kept];
    const seq = rt.emit(CH, { type: 'queue_changed', queue: { ...data.queue, rev: data.queue.rev + 1 } });
    return { r: 'queued', ch: CH, seq };
  });
}

describe('the page asks the runtime where a late message is', () => {
  it('a send answered without its event: the runtime says it was delivered long ago, and the bubble takes that', async () => {
    const { rt, m } = await openChat();
    answerFromMemory(rt, (id) =>
      record(id, { k: 'done', turnId: 'c:0f0e0d0c-0000-4000-8000-000000000001#0' }, 7),
    );
    const id = sendMessage(m, { conversationId: CONV, text: 'как дела' });
    await settle();
    // The answer came, the event did not: before, the bubble said «дольше обычного» from here on, for ever.
    expect(msg(id)).toMatchObject({ rev: 0, state: { k: 'submitting' } });
    await vi.advanceTimersByTimeAsync(1600);
    expect(rt.statusAsks).toEqual([[id]]);
    expect(msg(id)).toMatchObject({ rev: 7, state: { k: 'done' } });
  });

  it('after F5 mid-delivery the kept copy is sent again, answered from memory, and takes its true state', async () => {
    const { rt, m } = await openChat();
    // What the outbox kept in the browser: a message the runtime had not shown before the page went away.
    const id = '01a1747c-d1d1-76f3-ad33-edcdd1c219c8';
    putLocal({
      clientId: id,
      machineId: m,
      conversationId: CONV,
      text: 'как дела',
      attachments: [],
      mode: 'queue',
      kind: 'prompt',
      held: false,
      createdAt: machineNow(m) - 3_600_000,
      attempts: 0,
      state: { k: 'submitting' },
      rev: 0,
      stateAt: machineNow(m) - 3_600_000,
      refused: null,
      wait: null,
      issue: null,
    });
    answerFromMemory(rt, (cid) =>
      record(cid, { k: 'in-turn', turnId: 'c:0f0e0d0c-0000-4000-8000-000000000002#0' }, 5),
    );
    flushOutbox(m);
    await vi.advanceTimersByTimeAsync(1600);
    expect(msg(id)).toMatchObject({ rev: 5, state: { k: 'in-turn' } });
    expect(rt.commands.filter((c) => c.op === 'send')).toHaveLength(1);
  });

  it('the runtime does not know it: back to the queue over the field, not lost, not sent again by itself; Retry sends it', async () => {
    const { rt, m } = await openChat();
    answerFromMemory(rt, () => null);
    const id = sendMessage(m, { conversationId: CONV, text: 'как дела' });
    await vi.advanceTimersByTimeAsync(1600);
    expect(msg(id)).toMatchObject({
      rev: 0,
      held: true,
      state: { k: 'failed', code: 'unconfirmed', retryable: true },
    });
    expect(waitsInQueue(msg(id), 'idle')).toBe(true);
    await vi.advanceTimersByTimeAsync(5 * 60_000);
    expect(rt.commands.filter((c) => c.op === 'send')).toHaveLength(1);
    // The runtime's memory of the opId would answer the same id again: the words go as a new message, the old one goes.
    rt.handlers.set('send', (cmd, r) => r.defaultSend(cmd as Extract<typeof cmd, { op: 'send' }>));
    retryMessage(id);
    await settle();
    expect(msg(id).state.k).toBe('cancelled');
    const again = [...messagesStore.values()].find((v) => v.clientId !== id && v.text === 'как дела')!;
    expect(again).toMatchObject({ rev: 1, held: true, state: { k: 'queued' } });
    expect(rt.commands.filter((c) => c.op === 'send').map((c) => c.body.opId)).toEqual([id, again.clientId]);
  });

  it('expired: the runtime delivered it long ago and let it go; the copy goes', async () => {
    const { rt, m } = await openChat();
    answerFromMemory(rt, () => null);
    const id = sendMessage(m, { conversationId: CONV, text: 'как дела' });
    rt.forgotten.set(id, 1792600000000);
    await vi.advanceTimersByTimeAsync(1600);
    expect(messagesStore.has(id)).toBe(false);
  });

  it('a message typed into the agent and never moved on is asked about past the runtime’s term; a steer too', async () => {
    const { rt, m } = await openChat();
    const data = rt.convs.get(CONV)!;
    const wait = { slowAt: machineNow(m) + 3000, deadlineAt: machineNow(m) + 30_000 };
    applyMessage(m, record('m-typed', { k: 'in-input', wait }, 2));
    applyMessage(m, { ...record('m-steer', { k: 'in-input', wait }, 2), mode: 'steer' });
    // The runtime moved the first on; the steer still waits in the agent's queue. This page missed the frame.
    data.messages = [
      record('m-typed', { k: 'queued', position: 1, reason: 'stalled', wait: null }, 3),
      { ...record('m-steer', { k: 'in-input', wait }, 2), mode: 'steer' },
    ];
    await vi.advanceTimersByTimeAsync(30_000);
    expect(rt.statusAsks).toEqual([]);
    await vi.advanceTimersByTimeAsync(5_100);
    expect(ours(rt.statusAsks)).toEqual([['m-typed', 'm-steer']]);
    expect(msg('m-typed').state).toMatchObject({ k: 'queued', reason: 'stalled' });
    expect(waitsInQueue(msg('m-typed'), 'idle')).toBe(true);
    expect(msg('m-steer').state.k).toBe('in-input');
    // Still waiting: asked again, no oftener than every 30 s.
    await vi.advanceTimersByTimeAsync(STATUS_EVERY_MS);
    expect(ours(rt.statusAsks)).toEqual([['m-typed', 'm-steer'], ['m-steer']]);
  });

  it('a queued message whose frames the page missed is asked about too, and leaves the strip once done', async () => {
    const { rt, m } = await openChat();
    const data = rt.convs.get(CONV)!;
    applyMessage(m, {
      ...record('m-q', { k: 'queued', position: 1, reason: 'blocked-text', wait: null }, 2),
    });
    expect(waitsInQueue(msg('m-q'), 'working')).toBe(true);
    // The agent took it and answered; the frames that said so never came (the chat's channel was refused).
    data.messages = [record('m-q', { k: 'done', turnId: 'c:0f0e0d0c-0000-4000-8000-000000000003#0' }, 6)];
    await vi.advanceTimersByTimeAsync(35_100);
    expect(ours(rt.statusAsks)).toEqual([['m-q']]);
    expect(msg('m-q')).toMatchObject({ rev: 6, state: { k: 'done' } });
    expect(waitsInQueue(msg('m-q'), 'working')).toBe(false);
  });

  it('an older runtime without the status page decides nothing: the message waits and is asked about again later', async () => {
    const { rt, m } = await openChat();
    rt.statusPage = false;
    answerFromMemory(rt, () => null);
    const id = sendMessage(m, { conversationId: CONV, text: 'как дела' });
    await vi.advanceTimersByTimeAsync(1600);
    expect(msg(id)).toMatchObject({ rev: 0, state: { k: 'submitting' } });
    rt.statusPage = true;
    rt.convs
      .get(CONV)!
      .messages.push(record(id, { k: 'done', turnId: 'c:0f0e0d0c-0000-4000-8000-000000000003#0' }, 4));
    await vi.advanceTimersByTimeAsync(STATUS_EVERY_MS);
    expect(msg(id)).toMatchObject({ rev: 4, state: { k: 'done' } });
  });

  it('Retry of a stalled message is the runtime’s retry of the same clientId', async () => {
    const { rt, m } = await openChat();
    applyMessage(m, record('m-stalled', { k: 'queued', position: 1, reason: 'stalled', wait: null }, 3));
    expect(retryMessage('m-stalled')).not.toBeNull();
    await settle();
    expect(rt.commands.at(-1)).toMatchObject({
      op: 'retry',
      body: { clientId: 'm-stalled', conversationId: CONV },
    });
  });
});
