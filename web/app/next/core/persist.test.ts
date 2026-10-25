import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { idbKV, memoryKV, startPersistence, type Persistence } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { batch } from '@/next/core/store';
import { FakeRuntime, type FakeSocket } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { resetCore } from '@/next/core/testing/reset';
import { setFetch } from '@/next/core/net/http';
import { putMachines } from '@/next/stores/machines';
import { sendMessage, startOutbox } from '@/next/commands';
import { MEMORY_CHATS, recentConversations, touchConversation } from '@/next/stores/cache';
import { applyStep, ensureFeed, entryStore, feedStore, key } from '@/next/stores/conversation';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { applyMessage, messagesStore } from '@/next/stores/messages';
import type { ClientFrame, ConvId } from '@/next/protocol/v2';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;

let persistence: Persistence | null = null;
let stopOutbox: (() => void) | null = null;

beforeEach(useFakeTime);
afterEach(() => {
  persistence?.stop();
  stopOutbox?.();
  persistence = null;
  stopOutbox = null;
  hub.stop();
  vi.useRealTimers();
});

const subOf = (s: FakeSocket, ch: string) =>
  s.sent.filter((f): f is Extract<ClientFrame, { f: 'sub' }> => f.f === 'sub' && f.ch === ch).pop();

describe('F5', () => {
  it('keeps the draft, the unsent message and the chat; the first frame draws before the network', async () => {
    const kv = memoryKV();
    const rt = new FakeRuntime();
    const { m } = await bootFake({ rt });
    persistence = startPersistence(kv);
    await persistence.ready;
    stopOutbox = startOutbox();
    const release = hub.openConversation(m, CONV);
    await settle();
    setDraft(CONV, { text: 'полуготовая мысль', attachments: [] });
    rt.hang = true;
    const id = sendMessage(m, { conversationId: CONV, text: 'Отправится, когда вернётся связь' });
    await vi.advanceTimersByTimeAsync(1000);
    await persistence.flushNow();
    // The page is closed while the send has no answer; here its attempt ends at cmdTimeoutMs, before the next step.
    await vi.advanceTimersByTimeAsync(11500);
    expect(kv.dump().drafts.get(CONV)).toEqual({ text: 'полуготовая мысль', attachments: [] });
    expect(kv.dump().outbox.has(id)).toBe(true);
    expect(kv.dump().feeds.has(CONV)).toBe(true);
    release();

    // The reload: every store is empty, the page starts from what IndexedDB kept.
    persistence.stop();
    stopOutbox();
    resetCore();
    expect(messagesStore.size).toBe(0);
    persistence = startPersistence(kv);
    await persistence.ready;
    expect(draftsStore.get(CONV)?.text).toBe('полуготовая мысль');
    // After the reload it still waits for the machine, calmly (not failed), and the outbox keeps it for the next try.
    expect(messagesStore.get(id)).toMatchObject({ rev: 0, state: { k: 'submitting' }, issue: 'timeout' });
    const cached = feedStore.get(CONV)!;
    expect(cached).toMatchObject({ loaded: true, source: 'cached' });
    expect(cached.rows.length).toBeGreaterThan(0);
    expect(recentConversations()[0]).toBe(CONV);

    // The network comes back: the chat resumes from its cursor, the message goes with the same clientId.
    rt.hang = false;
    setFetch(rt.fetch);
    putMachines([rt.machine]);
    stopOutbox = startOutbox();
    hub.start([m], m);
    const rel = hub.openConversation(m, CONV);
    await settle();
    await settle();
    const sock = rt.opened[rt.opened.length - 1];
    expect(subOf(sock, CH)).toMatchObject({ epoch: rt.epoch, afterSeq: expect.any(Number) });
    expect(messagesStore.get(id)).toMatchObject({ rev: 1, state: { k: 'queued' } });
    const sends = rt.commands.filter((c) => c.op === 'send' && c.body.opId === id);
    expect(sends.length).toBeGreaterThanOrEqual(1);
    expect(rt.ring(CH).events.filter((e) => e.e.type === 'message_upsert')).toHaveLength(1);
    await persistence.flushNow();
    expect(kv.dump().outbox.has(id)).toBe(false);
    rel();
  });

  it('a message cached as queued that the runtime finished meanwhile is gone once the chat is back', async () => {
    const kv = memoryKV();
    const rt = new FakeRuntime();
    const { m } = await bootFake({ rt });
    const cachedIds = () =>
      ((kv.dump().feeds.get(CONV) as { messages?: { clientId: string }[] } | undefined)?.messages ?? []).map(
        (x) => x.clientId,
      );
    persistence = startPersistence(kv);
    await persistence.ready;
    const release = hub.openConversation(m, CONV);
    await settle();
    // As on prod: sent with a picture while the agent was busy, the page kept it queued{agent-busy} and went away.
    const id = '01a170d2-4226-7110-a953-4ec030b1c0cc';
    applyMessage(m, {
      clientId: id,
      conversationId: CONV,
      text: 'И ещё, у меня почти полностью закончилась память',
      attachments: [{ id: 'up_5GURGK5YLFFX2MQ4', name: 'CleanShot.png', kind: 'image', mime: 'image/png' }],
      mode: 'queue',
      kind: 'prompt',
      createdAt: 1792894190386,
      attempts: 0,
      state: { k: 'queued', position: 2, reason: 'behind', wait: null },
      rev: 4,
    });
    await vi.advanceTimersByTimeAsync(1000);
    await persistence.flushNow();
    expect(cachedIds()).toContain(id);
    release();

    persistence.stop();
    resetCore();
    persistence = startPersistence(kv);
    await persistence.ready;
    // The browser's cache brings it back into the queue before the network.
    expect(messagesStore.get(id)?.state.k).toBe('queued');

    // The runtime answered it long ago: its snapshot lists only the messages not over, and this one is not among them.
    setFetch(rt.fetch);
    putMachines([rt.machine]);
    hub.start([m], m);
    const rel = hub.openConversation(m, CONV);
    await settle();
    await settle();
    expect(messagesStore.get(id)).toBeUndefined();
    await persistence.flushNow();
    expect(cachedIds()).not.toContain(id);
    rel();
  });

  it('works without IndexedDB: nothing kept, nothing thrown', async () => {
    await bootFake({ start: false });
    persistence = startPersistence(idbKV('none'));
    await persistence.ready;
    setDraft(CONV, { text: 'x', attachments: [] });
    await vi.advanceTimersByTimeAsync(500);
    await persistence.flushNow();
    expect(draftsStore.get(CONV)?.text).toBe('x');
  });

  it('writes a draft 300 ms after the last keystroke', async () => {
    const kv = memoryKV();
    await bootFake({ start: false });
    persistence = startPersistence(kv);
    await persistence.ready;
    setDraft(CONV, { text: 'a', attachments: [] });
    await vi.advanceTimersByTimeAsync(200);
    setDraft(CONV, { text: 'ab', attachments: [] });
    await vi.advanceTimersByTimeAsync(200);
    expect(kv.dump().drafts.has(CONV)).toBe(false);
    await vi.advanceTimersByTimeAsync(100);
    await settle();
    expect(kv.dump().drafts.get(CONV)).toEqual({ text: 'ab', attachments: [] });
  });
});

describe('the chat cache', () => {
  it('keeps 12 chats in memory; an open one is never let go', async () => {
    await bootFake({ start: false });
    const ids = Array.from({ length: MEMORY_CHATS + 2 }, (_, i) => `tab-c${i}` as ConvId);
    const held = hub.openConversation('runner-2', ids[0]);
    batch(() => {
      for (const id of ids) {
        ensureFeed(id, 'runner-2');
        applyStep(id, { id: 'x:1#0', turnId: null, at: 1, rev: 1, t: 'interrupt' }, null);
        touchConversation(id);
      }
    });
    expect(feedStore.has(ids[0])).toBe(true);
    expect(feedStore.has(ids[1])).toBe(false);
    expect(entryStore.has(key(ids[1], 'x:1#0'))).toBe(false);
    expect(feedStore.has(ids[2])).toBe(true);
    expect(recentConversations(100)).toHaveLength(MEMORY_CHATS + 1);
    held();
  });
});
