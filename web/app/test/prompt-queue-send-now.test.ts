import { afterEach, describe, expect, it, vi } from 'vitest';
import { composerRoute, isSendNowKey, sendNowShortcut } from '@/lib/hl-send-shortcut';

vi.mock('@/platform/config', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/platform/config')>();
  return { ...real, appBasePath: () => '', platformApi: () => '/api/platform' };
});

const { enqueuePrompt, isGoneFromQueue, steerQueuedPrompt, usePromptQueueStore } =
  await import('@/hooks/use-prompt-queue');

const answer = (status: number, body: unknown) =>
  vi.fn(async () => new Response(JSON.stringify(body), { status }));

afterEach(() => {
  vi.unstubAllGlobals();
  usePromptQueueStore.setState({ server: {}, local: {}, queues: {} });
});

const key = (init: Partial<Parameters<typeof isSendNowKey>[0]>) =>
  isSendNowKey({ key: 'Enter', metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...init });

describe('send now (⌘↵)', () => {
  it('is Enter with Cmd or Ctrl, and nothing else', () => {
    expect(key({ metaKey: true })).toBe(true);
    expect(key({ ctrlKey: true })).toBe(true);
    expect(key({})).toBe(false);
    expect(key({ metaKey: true, shiftKey: true })).toBe(false);
    expect(key({ ctrlKey: true, altKey: true })).toBe(false);
    expect(key({ metaKey: true, key: 'a' })).toBe(false);
    expect(sendNowShortcut(true)).toBe('⌘↵');
    expect(sendNowShortcut(false)).toBe('Ctrl+↵');
  });

  it('only a working agent is sent to at once; a message to an idle one goes straight to it', () => {
    const base = { queueOnServer: true, slashLine: false, holding: false, resumable: false };
    expect(composerRoute({ ...base, agentWorking: true, now: false })).toBe('queue');
    expect(composerRoute({ ...base, agentWorking: true, now: true })).toBe('send-now');
    expect(composerRoute({ ...base, agentWorking: false, now: true })).toBe('direct');
    expect(composerRoute({ ...base, agentWorking: false, now: false })).toBe('direct');
  });
});

describe('enqueuePrompt', () => {
  it('asks the runtime to steer only for send now, and gives back what became of the message', async () => {
    const item = { id: 'q1', text: 'hi', images: [], createdAt: 1 };
    const fetchMock = answer(200, { item, delivery: 'steered' });
    vi.stubGlobal('fetch', fetchMock);
    const sent = await enqueuePrompt(undefined, 'tab-1', 'hi', [], async () => ({ text: 'hi', images: [] }), {
      now: true,
    });
    expect(sent).toEqual({ item, delivery: 'steered' });
    const body = JSON.parse(String((fetchMock.mock.calls[0] as unknown as [string, RequestInit])[1].body));
    expect(body).toEqual({
      tabId: 'tab-1',
      action: 'add',
      text: 'hi',
      images: [],
      clientId: expect.stringMatching(/^c-/),
      now: true,
    });
  });

  it('leaves `now` out for Enter and shows the message in the queue until the runtime answers', async () => {
    let seen = 0;
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init: RequestInit) => {
        seen = usePromptQueueStore.getState().queues['tab-1']?.length ?? 0;
        expect(JSON.parse(String(init.body))).not.toHaveProperty('now');
        return new Response(
          JSON.stringify({ item: { id: 'q2', text: 'x', images: [], createdAt: 2 }, delivery: 'queued' }),
        );
      }),
    );
    const sent = await enqueuePrompt(undefined, 'tab-1', 'x', [], async () => ({ text: 'x', images: [] }));
    expect(seen).toBe(1);
    expect(sent.delivery).toBe('queued');
    expect(usePromptQueueStore.getState().queues['tab-1']).toBeUndefined();
  });

  it('leaves nothing behind when the runtime refuses', async () => {
    vi.stubGlobal('fetch', answer(409, { error: 'nope' }));
    await expect(
      enqueuePrompt(undefined, 'tab-1', 'x', [], async () => ({ text: 'x', images: [] })),
    ).rejects.toThrow('nope');
    expect(usePromptQueueStore.getState().queues['tab-1']).toBeUndefined();
  });
});

describe('a steer after the agent took the message', () => {
  it('is recognised in both languages of the runtime, and only that', async () => {
    for (const message of ['The message is no longer in the queue', 'Сообщения уже нет в очереди']) {
      vi.stubGlobal('fetch', answer(409, { error: message }));
      const failure = await steerQueuedPrompt(undefined, 'tab-1', 'q1').catch((error: unknown) => error);
      expect(isGoneFromQueue(failure)).toBe(true);
    }
    vi.stubGlobal('fetch', answer(500, { error: 'The agent did not accept the message' }));
    const other = await steerQueuedPrompt(undefined, 'tab-1', 'q1').catch((error: unknown) => error);
    expect(isGoneFromQueue(other)).toBe(false);
  });
});
