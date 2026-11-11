import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  cancelQueuedChat,
  queueChat,
  queuedChatId,
  queuedChatsStore,
  queuedCreate,
  queuedItemOf,
  waiting,
  watchQueuedChats,
  type QueuedChat,
} from '@/next/commands/queued-chats';
import { flush } from '@/next/core/store';

const item = (over: Partial<QueuedChat> = {}): QueuedChat => ({
  id: 'q1',
  machineId: 'm1',
  clientId: 'c1',
  text: 'сделай отчёт',
  state: 'queued',
  error: null,
  convId: null,
  createdAt: 1,
  ...over,
});

const respond = (body: unknown, status = 200) =>
  Promise.resolve(
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  );

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn());
  queuedChatsStore.set(null);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

// with none of the person's machines answering, the first message of a new chat is left with the hub.
describe('a new chat that waits for its machine', () => {
  it('is the page’s own conversation.create, one opId that is the first message’s clientId, nothing attached', () => {
    const body = queuedCreate(
      {
        where: { cwd: '/w' },
        provider: 'claude',
        model: null,
        first: { text: 'привет', attachments: [], refs: [] },
      },
      'c-9',
    );
    expect(body).toEqual({
      opId: 'c-9',
      cwd: '/w',
      provider: 'claude',
      model: null,
      first: { clientId: 'c-9', text: 'привет', attachmentIds: [] },
    });
  });

  it('goes to the hub for the machine and waits in the list at once', async () => {
    vi.mocked(fetch).mockImplementation(() => respond({ item: item() }, 201));
    const res = await queueChat('m1', {
      where: { cwd: '/w' },
      provider: 'claude',
      model: null,
      first: { text: 'сделай отчёт' },
    });
    expect(res).toEqual({ ok: true, item: item() });
    const [url, init] = vi.mocked(fetch).mock.calls[0];
    expect(String(url)).toMatch(/\/offline-chats\?machine=m1$/);
    expect(init?.method).toBe('POST');
    const sent = JSON.parse(String(init?.body)) as { create: { opId: string; first: { clientId: string } } };
    expect(sent.create.opId).toBe(sent.create.first.clientId);
    flush();
    expect(queuedChatsStore.get()).toEqual([item()]);
  });

  it('says so when the hub did not take it', async () => {
    vi.mocked(fetch).mockImplementation(() => respond({ error: 'full' }, 409));
    const res = await queueChat('m1', {
      where: { cwd: '/w' },
      provider: 'claude',
      model: null,
      first: { text: 'x' },
    });
    expect(res.ok).toBe(false);
  });

  it('is taken back by the person', async () => {
    queuedChatsStore.set([item(), item({ id: 'q2' })]);
    vi.mocked(fetch).mockImplementation(() => respond({ item: item() }));
    expect(await cancelQueuedChat('q1')).toBe(true);
    expect(String(vi.mocked(fetch).mock.calls[0][0])).toMatch(/\/offline-chats\?id=q1$/);
    flush();
    expect(queuedChatsStore.get()?.map((x) => x.id)).toEqual(['q2']);
  });

  it('has an address of its own that is no machine’s chat', () => {
    expect(queuedItemOf(queuedChatId('q1'))).toBe('q1');
    expect(queuedItemOf('tab-abc')).toBeNull();
  });

  it('is asked of the hub while it waits, and no longer once the machine made it', async () => {
    vi.useFakeTimers();
    const answers = [{ items: [item()] }, { items: [item({ state: 'delivered', convId: 'tab-new' })] }];
    vi.mocked(fetch).mockImplementation(() => respond(answers.shift() ?? { items: [] }));
    const stop = watchQueuedChats();
    await vi.advanceTimersByTimeAsync(0);
    expect(waiting(queuedChatsStore.get())).toBe(true);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(queuedChatsStore.get()?.[0]).toMatchObject({ state: 'delivered', convId: 'tab-new' });
    await vi.advanceTimersByTimeAsync(20_000);
    expect(vi.mocked(fetch)).toHaveBeenCalledTimes(2);
    stop();
  });
});
