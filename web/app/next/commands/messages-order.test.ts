import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { machineNow } from '@/next/core/clock';
import { hub } from '@/next/core/realtime/hub';
import { fixture } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { cancelMessage, flushOutbox, retryMessage, sendMessage } from '@/next/commands';
import { byMaking, madeAt, waitsItsTurn } from '@/next/commands/send-line';
import { messagesStore, returnToQueue } from '@/next/stores/messages';
import type { Command, ConvId, MessageState } from '@/next/protocol/v2';
import type { FakeRuntime } from '@/next/core/testing/fake-runtime';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const msg = (id: string) => messagesStore.get(id)!;
const stateOf = (name: string): MessageState => fixture<{ e: { state: MessageState } }>(name).e.state;

/** The commands of one op the runtime got, as the clientIds they name, in the order they came. */
const got = (rt: FakeRuntime, op: Command['op']): string[] =>
  rt.commands.filter((c) => c.op === op).map((c) => (c.body as { clientId: string }).clientId);

beforeEach(useFakeTime);
afterEach(() => {
  hub.stop();
  vi.useRealTimers();
});

async function openChat() {
  const boot = await bootFake();
  hub.openConversation(boot.m, CONV);
  await settle();
  return boot;
}

describe('the messages of one chat leave the page in the order they were written', () => {
  it('reads the time of a clientId, and keeps the order of making for ids of one millisecond', () => {
    expect(madeAt('01900000-0001-7000-8000-000000000001')).toBe(0x019000000001);
    expect(madeAt('legacy-7')).toBeNull();
    const view = (clientId: string) => ({ clientId, createdAt: 1 }) as Parameters<typeof byMaking>[0];
    const early = view('01900000-0001-7000-8000-00000000000f');
    const late = view('01900000-0002-7000-8000-000000000001');
    expect(byMaking(early, late)).toBeLessThan(0);
    expect(byMaking(late, early)).toBeGreaterThan(0);
    expect(byMaking(early, view('01900000-0001-7000-8000-000000000001'))).toBe(0);
  });

  it('the second waits for the first to be answered: a lost try of the first is not overtaken', async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 1;
    const a = sendMessage(m, { conversationId: CONV, text: 'первое' });
    const b = sendMessage(m, { conversationId: CONV, text: 'второе' });
    await settle();
    // The first try of the first got the gateway's 502; the second has not left the page.
    expect(msg(a)).toMatchObject({ state: { k: 'submitting' }, issue: 'http' });
    expect(got(rt, 'send')).toEqual([]);
    expect(waitsItsTurn(msg(b))).toBe(true);
    expect(waitsItsTurn(msg(a))).toBe(false);
    await vi.advanceTimersByTimeAsync(1100);
    await settle();
    expect(got(rt, 'send')).toEqual([a, b]);
    expect(msg(a).rev).toBe(1);
    expect(msg(b).rev).toBe(1);
  });

  it('after the socket is live again the whole line goes, one by one, in order', async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 1;
    const ids = ['раз', 'два', 'три'].map((text) => sendMessage(m, { conversationId: CONV, text }));
    await settle();
    expect(got(rt, 'send')).toEqual([]);
    flushOutbox(m);
    await vi.advanceTimersByTimeAsync(50);
    await settle();
    expect(got(rt, 'send')).toEqual(ids);
  });

  it('a first one refused on the merits does not hold up the next', async () => {
    const { rt, m } = await openChat();
    let refuse = true;
    rt.handlers.set('send', (cmd, r) => {
      if (!refuse) return r.defaultSend(cmd as Extract<typeof cmd, { op: 'send' }>);
      refuse = false;
      return { r: 'refused', code: 'text-too-long' };
    });
    const a = sendMessage(m, { conversationId: CONV, text: 'слишком длинное' });
    const b = sendMessage(m, { conversationId: CONV, text: 'обычное' });
    await vi.advanceTimersByTimeAsync(50);
    await settle();
    expect(msg(a).state.k).toBe('failed');
    expect(msg(b)).toMatchObject({ rev: 1, state: { k: 'queued' } });
    expect(got(rt, 'send')).toEqual([a, b]);
  });
});

describe('a message taken back', () => {
  it('is cancelled only by the runtime: the cancel goes after the send that was on its way', async () => {
    const { rt, m } = await openChat();
    rt.latencyMs = 200;
    rt.handlers.set('cancel', (cmd, r) => ({
      r: 'accepted',
      ch: CH,
      seq: r.emit(CH, {
        type: 'prompt_state',
        clientId: (cmd.body as { clientId: string }).clientId,
        state: stateOf('ev.prompt_state-cancelled'),
        rev: 2,
      }),
    }));
    const id = sendMessage(m, { conversationId: CONV, text: 'передумал' });
    await vi.advanceTimersByTimeAsync(50);
    const h = cancelMessage(id)!;
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, cancelling: true });
    // Pressed twice: one cancel.
    expect(cancelMessage(id)).toBeNull();
    await vi.advanceTimersByTimeAsync(1000);
    await settle();
    expect((await h.done).k).toBe('done');
    expect(rt.commands.map((c) => c.op).filter((op) => op === 'send' || op === 'cancel')).toEqual([
      'send',
      'cancel',
    ]);
    expect(msg(id).state).toEqual({ k: 'cancelled', by: 'user' });
    expect(msg(id).cancelling).toBeUndefined();
  });

  it('that the agent already has stays what the runtime says, and the cancel fails', async () => {
    const { rt, m } = await openChat();
    rt.latencyMs = 200;
    rt.handlers.set('cancel', () => ({ r: 'refused', code: 'too-late' }));
    const id = sendMessage(m, { conversationId: CONV, text: 'уже ушло' });
    await vi.advanceTimersByTimeAsync(50);
    const h = cancelMessage(id)!;
    await vi.advanceTimersByTimeAsync(1000);
    await settle();
    expect((await h.done).k).toBe('failed');
    expect(msg(id)).toMatchObject({ rev: 1, state: { k: 'queued' } });
    expect(msg(id).cancelling).toBeUndefined();
  });

  it('before any try of it left the page goes at once, and the line moves on without it', async () => {
    const { rt, m } = await openChat();
    rt.gatewayErrors = 1;
    const a = sendMessage(m, { conversationId: CONV, text: 'первое' });
    const b = sendMessage(m, { conversationId: CONV, text: 'лишнее' });
    const c = sendMessage(m, { conversationId: CONV, text: 'третье' });
    await settle();
    const h = cancelMessage(b)!;
    await settle();
    expect((await h.done).k).toBe('done');
    expect(msg(b).state).toEqual({ k: 'cancelled', by: 'user' });
    await vi.advanceTimersByTimeAsync(1100);
    await settle();
    expect(got(rt, 'send')).toEqual([a, c]);
    expect(got(rt, 'cancel')).toEqual([]);
  });
});

describe('Повторить of a message the runtime said it does not know', () => {
  it('sends it under the same clientId first, and as a new message only when that came back unknown too', async () => {
    const { rt, m } = await openChat();
    const id = sendMessage(m, { conversationId: CONV, text: 'куда делось' });
    await settle();
    // The runtime answered the send, and its status says it has no such message.
    returnToQueue(id, machineNow(m));
    expect(msg(id).state).toMatchObject({ k: 'failed', code: 'unconfirmed' });
    const before = messagesStore.size;

    expect(retryMessage(id)).toBeNull();
    expect(msg(id)).toMatchObject({ state: { k: 'submitting' }, attempts: 1 });
    await settle();
    expect(messagesStore.size).toBe(before);
    expect(got(rt, 'send')).toEqual([id, id]);

    returnToQueue(id, machineNow(m));
    retryMessage(id);
    await settle();
    expect(msg(id).state).toEqual({ k: 'cancelled', by: 'user' });
    expect(messagesStore.size).toBe(before + 1);
    const sends = got(rt, 'send');
    expect(sends.length).toBe(3);
    expect(sends[2]).not.toBe(id);
  });
});
