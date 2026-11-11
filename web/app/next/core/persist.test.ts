import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  FEED_KEPT_MS,
  FEEDS_DELAY_MS,
  FEEDS_KEPT_MAX,
  feedsToDrop,
  feedsToRestore,
  idbKV,
  memoryKV,
  startPersistence,
  type KV,
  type Persistence,
} from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { batch } from '@/next/core/store';
import { fixture, FakeRuntime, type FakeSocket } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { resetCore } from '@/next/core/testing/reset';
import { setFetch } from '@/next/core/net/http';
import { putMachines } from '@/next/stores/machines';
import { sendMessage, startOutbox } from '@/next/commands';
import { MEMORY_CHATS, recentConversations, touchConversation } from '@/next/stores/cache';
import { applyStep, ensureFeed, entryStore, feedStore, key } from '@/next/stores/conversation';
import { draftsStore, setDraft } from '@/next/stores/drafts';
import { applyMessage, messagesStore } from '@/next/stores/messages';
import type { ClientFrame, ConvEvent, ConvId, Entry } from '@/next/protocol/v2';

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

describe('what the cache writes, and when', () => {
  /** A new step of the live turn: the feed changes as it does while the agent answers. */
  const step = (n: number): ConvEvent => {
    const e = fixture<{ e: Extract<ConvEvent, { type: 'step' }> }>('ev.step').e;
    return { ...e, entry: { ...e.entry, id: `x:${40000 + n}#0`, at: e.entry.at + n } as Entry, counts: null };
  };
  /** A store that tells which cached chats were written. */
  const watched = () => {
    const kv = memoryKV();
    const feeds: string[] = [];
    const convs: string[] = [];
    const spy: KV = {
      ...kv,
      set: async (area, k, v) => {
        if (area === 'feeds') feeds.push(k);
        if (area === 'convs') convs.push(k);
        await kv.set(area, k, v);
      },
    };
    return { kv, spy, feeds, convs };
  };

  it('while the agent answers, the chat is written once in five seconds, and only that chat', async () => {
    const { spy, feeds } = watched();
    const rt = new FakeRuntime();
    const { m } = await bootFake({ rt });
    persistence = startPersistence(spy);
    await persistence.ready;
    const release = hub.openConversation(m, CONV);
    await settle();
    await persistence.flushNow();
    feeds.length = 0;
    for (let i = 1; i <= 20; i++) {
      rt.emit(CH, step(i));
      await vi.advanceTimersByTimeAsync(500);
    }
    // Ten seconds of an answer: two writes, both of the chat that changed.
    expect(feeds).toEqual([CONV, CONV]);
    expect(FEEDS_DELAY_MS).toBeGreaterThanOrEqual(5000);
    // Leaving the page writes what waits at once.
    rt.emit(CH, step(21));
    await persistence.flushNow();
    expect(feeds).toEqual([CONV, CONV, CONV]);
    release();
  });

  it('leaves alone what another window keeps, and lets go only what is old', async () => {
    const { kv, spy } = watched();
    const { m } = await bootFake();
    persistence = startPersistence(spy);
    await persistence.ready;
    // Another window of the same browser keeps a chat this page never opened; a third one was kept long ago.
    await kv.set('feeds', 'tab-elsewhere', { convId: 'tab-elsewhere', rank: 0, at: Date.now() });
    await kv.set('feeds', 'tab-forgotten', {
      convId: 'tab-forgotten',
      rank: 1,
      at: Date.now() - FEED_KEPT_MS - 1,
    });
    const release = hub.openConversation(m, CONV);
    await settle();
    await persistence.flushNow();
    expect([...kv.dump().feeds.keys()].sort()).toEqual([CONV, 'tab-elsewhere'].sort());
    release();
  });

  it('only the list of the machine something changed on is written', async () => {
    const { conversationsStore } = await import('@/next/stores/conversations');
    const { spy, convs } = watched();
    const { m } = await bootFake();
    persistence = startPersistence(spy);
    await persistence.ready;
    await persistence.flushNow();
    convs.length = 0;
    await vi.advanceTimersByTimeAsync(60_000);
    expect(convs).toEqual([]);
    const c = conversationsStore.get(CONV)!;
    for (let i = 1; i <= 8; i++) {
      conversationsStore.set(CONV, { ...c, title: `title ${i}` });
      await vi.advanceTimersByTimeAsync(1000);
    }
    expect(convs).toEqual([`list:${m}`]);
  });

  it('lets go by age and by count, never by «not in my list»', () => {
    const now = 1_000_000_000_000;
    const fresh = (k: string, ago: number) => [k, { at: now - ago }] as const;
    expect(feedsToDrop([fresh('a', 1000), fresh('b', FEED_KEPT_MS + 1)], [], now)).toEqual(['b']);
    const many = Array.from({ length: FEEDS_KEPT_MAX + 2 }, (_, i) => fresh(`c${i}`, i * 1000));
    expect(feedsToDrop(many, [], now)).toEqual([`c${FEEDS_KEPT_MAX}`, `c${FEEDS_KEPT_MAX + 1}`]);
    // What an older page kept has no time: it stays only while this page keeps that chat itself.
    const legacy = [
      ['mine', {}],
      ['theirs', {}],
    ] as const;
    expect(feedsToDrop(legacy, ['mine'], now)).toEqual(['theirs']);
  });

  it('takes back the six written last, the most recent one last', () => {
    const kept = Array.from({ length: 9 }, (_, i) => ({ convId: `c${i}`, rank: 0, at: 100 + i }));
    const legacy = { convId: 'old', rank: 0 };
    const back = feedsToRestore([legacy, ...kept] as never);
    expect(back.map((f) => f.convId)).toEqual(['c3', 'c4', 'c5', 'c6', 'c7', 'c8']);
    expect(
      feedsToRestore([
        { convId: 'r1', rank: 1 },
        { convId: 'r0', rank: 0 },
      ] as never).map((f) => f.convId),
    ).toEqual(['r1', 'r0']);
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

describe('the kept list (HRN-902)', () => {
  const conv = (id: string, machineId: string) =>
    ({
      id,
      machineId,
      title: id,
      lastActivityAt: 1,
      pinned: false,
    }) as unknown as import('@/next/protocol/v2').Conversation;

  it('keeps every machine whose own list came, and draws them all on the next load', async () => {
    const { conversationsStore, convsCachedStore, convsLoadedStore } =
      await import('@/next/stores/conversations');
    const kv = memoryKV();
    // A page of an older version kept only its active machine's list, under one key.
    await kv.set('convs', 'list', {
      machineId: 'c',
      conversations: [conv('tab-c1', 'c')],
      projects: [],
      defaultCwd: null,
    });
    persistence = startPersistence(kv);
    await persistence.ready;
    expect(convsCachedStore.has('c')).toBe(true);
    batch(() => {
      conversationsStore.set('tab-a1' as ConvId, conv('tab-a1', 'a'));
      conversationsStore.set('tab-b1' as ConvId, conv('tab-b1', 'b'));
      convsLoadedStore.set('a', true);
      convsLoadedStore.set('b', true);
    });
    await vi.advanceTimersByTimeAsync(1500);
    await persistence.flushNow();
    const kept = kv.dump().convs;
    expect([...kept.keys()]).toEqual(expect.arrayContaining(['list:a', 'list:b']));
    // The older page's one list went once each machine's was written.
    expect([...kept.keys()]).not.toContain('list');

    persistence.stop();
    resetCore();
    persistence = startPersistence(kv);
    await persistence.ready;
    expect(convsCachedStore.has('a') && convsCachedStore.has('b')).toBe(true);
    expect(conversationsStore.has('tab-a1' as ConvId) && conversationsStore.has('tab-b1' as ConvId)).toBe(
      true,
    );
  });
});
