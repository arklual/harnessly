// @vitest-environment jsdom
// F5 with a cached chat: the runtime answers from the cursor with the events the page missed, not a snapshot, so the
// parts of a conversation that only the snapshot carries (gates, the queue, «Сейчас», the runtime's messages) come
// from the cache that kept them with the cursor. A cache from before that has no such state and resumes by a snapshot.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { memoryKV, startPersistence, type KV, type Persistence } from '@/next/core/persist';
import { setFetch } from '@/next/core/net/http';
import { hub } from '@/next/core/realtime/hub';
import { resetCore } from '@/next/core/testing/reset';
import type { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import type { ClientId, ConvId, Message } from '@/next/protocol/v2';
import { feedStore, gatesStore, nowStore, openGates, queueStore } from '@/next/stores/conversation';
import { putMachines } from '@/next/stores/machines';
import { messagesStore } from '@/next/stores/messages';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const CH = `conv:${CONV}` as const;
const QUESTION = 'q:toolu_01AskQ';
const MINE = '0192b6f1-0000-7000-8000-000000000002' as ClientId;

let persistence: Persistence | null = null;

beforeEach(useFakeTime);
afterEach(() => {
  persistence?.stop();
  persistence = null;
  hub.stop();
  vi.useRealTimers();
});

/** The page with the chat open, saved to the cache, and closed. */
async function openAndSave(kv: KV): Promise<{ rt: FakeRuntime; m: string }> {
  const { rt, m } = await bootFake();
  persistence = startPersistence(kv);
  await persistence.ready;
  const close = hub.openConversation(m, CONV);
  await settle();
  return { rt, m: m as string, ...(await save(close)) };
}

async function save(close: () => void): Promise<object> {
  await vi.advanceTimersByTimeAsync(1500);
  await persistence!.flushNow();
  close();
  await settle();
  return {};
}

/** F5: every store is empty, the runtime is the same one (it replays, then an empty snapshot "after" says it is done). */
async function reload(rt: FakeRuntime, kv: KV, m: string): Promise<() => void> {
  persistence!.stop();
  resetCore();
  persistence = startPersistence(kv);
  await persistence.ready;
  setFetch(rt.fetch);
  putMachines([rt.machine]);
  hub.start([m as never], m as never);
  return hub.openConversation(m as never, CONV);
}

describe('F5 on a chat the runtime resumes by replay', () => {
  it('keeps an open question and a permission, and the replay moves them', async () => {
    const kv = memoryKV();
    const { rt, m } = await openAndSave(kv);
    const open = openGates(gatesStore.get(CONV)!);
    expect(open.questions.map((q) => q.id)).toEqual([QUESTION]);
    expect(open.permissions).toHaveLength(1);
    const permission = open.permissions[0].id;

    persistence!.stop();
    resetCore();
    expect(gatesStore.get(CONV)).toBeUndefined();
    // While the page was away the question was answered from another device.
    rt.emit(CH, { type: 'gate_updated', id: QUESTION, state: { k: 'answered', at: 5, byDevice: 'phone' } });
    const again = await reload(rt, kv, m);
    await settle();
    await settle();
    const gates = gatesStore.get(CONV)!;
    expect(gates.questions.find((q) => q.id === QUESTION)?.state.k).toBe('answered');
    expect(openGates(gates).permissions.map((p) => p.id)).toEqual([permission]);
    again();
  });

  it('keeps the queue and the messages that stand in it', async () => {
    const kv = memoryKV();
    const { rt, m } = await openAndSave(kv);
    const queued: Message = {
      clientId: MINE,
      conversationId: CONV,
      text: 'Потом проверь схему',
      attachments: [],
      mode: 'queue',
      kind: 'prompt',
      createdAt: 1,
      attempts: 1,
      state: { k: 'queued', position: 1, reason: 'agent-busy', wait: null },
      rev: 3,
    };
    rt.emit(CH, { type: 'message_upsert', message: queued });
    await settle();
    await vi.advanceTimersByTimeAsync(1500);
    await persistence!.flushNow();
    const items = queueStore.get(CONV)!.items;
    expect(items.length).toBeGreaterThan(0);

    const again = await reload(rt, kv, m);
    expect(queueStore.get(CONV)?.items).toEqual(items);
    expect(messagesStore.get(MINE)?.text).toBe('Потом проверь схему');
    // The replay then moves it: the runtime takes it into the input.
    rt.emit(CH, { type: 'prompt_state', clientId: MINE, state: { k: 'accepted', entryId: null }, rev: 4 });
    await settle();
    await settle();
    expect(messagesStore.get(MINE)?.state.k).toBe('accepted');
    again();
  });

  it('keeps «Сейчас» of a working agent', async () => {
    const kv = memoryKV();
    const { rt, m } = await openAndSave(kv);
    const now = nowStore.get(CONV);
    expect(now?.thought).toBeTruthy();

    const again = await reload(rt, kv, m);
    expect(nowStore.get(CONV)).toEqual(now);
    rt.emit(CH, { type: 'now', ...now!, thought: 'Дальше', at: now!.at + 1000 });
    await settle();
    await settle();
    expect(nowStore.get(CONV)?.thought).toBe('Дальше');
    again();
  });

  it('takes the cached feed for caught up once the replay ends, even a replay of nothing', async () => {
    const kv = memoryKV();
    const { rt, m } = await openAndSave(kv);
    const rows = feedStore.get(CONV)!.rows;

    const again = await reload(rt, kv, m);
    await settle();
    await settle();
    expect(feedStore.get(CONV)?.source).toBe('live');
    expect(feedStore.get(CONV)?.rows).toEqual(rows);
    again();
  });

  it('resumes by a snapshot when the cache was kept before the state was: nothing is replayed over nothing', async () => {
    const kv = memoryKV();
    const { rt, m } = await openAndSave(kv);
    const kept = kv.dump().feeds.get(CONV) as { state?: unknown };
    expect(kept.state).toBeDefined();
    delete kept.state;

    const again = await reload(rt, kv, m);
    await settle();
    await settle();
    expect(openGates(gatesStore.get(CONV)!).questions.map((q) => q.id)).toEqual([QUESTION]);
    expect(nowStore.get(CONV)).toBeDefined();
    expect(queueStore.get(CONV)).toBeDefined();
    again();
  });
});
