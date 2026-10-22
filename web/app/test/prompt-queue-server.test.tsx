// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { THandoffEvent } from '@/lib/hl-queue-handoff';

vi.mock('@/platform/config', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/platform/config')>();
  return { ...real, appBasePath: () => '', platformApi: () => '/api/platform' };
});

const { onStatusMessage } = await import('@/hooks/use-agent-status');
const {
  enqueuePrompt,
  isGoneFromQueue,
  mergeQueue,
  onQueueHandoff,
  QueueRequestError,
  retryQueuedPrompt,
  usePromptQueueStore,
} = await import('@/hooks/use-prompt-queue');
const { default: useBackgroundTasks } = await import('@/hooks/use-background-tasks');
const { default: PromptQueueStack } = await import('@/components/agents/composer/prompt-queue-stack');

const shown = (tabId: string) => usePromptQueueStore.getState().queues[tabId];
const status = (message: object) => onStatusMessage(JSON.stringify(message));
const item = (id: string, text: string, extra: object = {}) => ({
  id,
  text,
  images: [],
  createdAt: 1,
  ...extra,
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  usePromptQueueStore.setState({ server: {}, local: {}, queues: {} });
  status({ type: 'background:sync', tabs: {} });
});

describe('the status socket feeds the stores', () => {
  it('puts the runtime queue in the store on connect and on every change', () => {
    status({ type: 'prompt-queue:sync', queues: { t1: [item('q1', 'a')], t2: [item('q2', 'b')] } });
    expect(shown('t1')).toMatchObject([{ id: 'q1' }]);
    expect(shown('t2')).toMatchObject([{ id: 'q2' }]);
    status({
      type: 'prompt-queue:update',
      tabId: 't1',
      items: [item('q1', 'a', { status: 'failed', reason: 'no-tab' })],
    });
    expect(shown('t1')).toMatchObject([{ id: 'q1', status: 'failed', reason: 'no-tab' }]);
    status({ type: 'prompt-queue:update', tabId: 't2', items: [] });
    expect(shown('t2')).toBeUndefined();
  });

  it('puts background tasks in theirs', () => {
    const { result } = renderHook(() => useBackgroundTasks('t1'));
    const task = { id: 'b1', kind: 'shell', label: 'npm run dev', startedAt: 1 };
    act(() => status({ type: 'background:sync', tabs: { t1: [task] } }));
    expect(result.current).toEqual([task]);
    act(() => status({ type: 'background:update', tabId: 't1', tasks: [] }));
    expect(result.current).toEqual([]);
  });
});

describe('a message this page adds', () => {
  it('shows once: its local chip gives way to the runtime item with its client id', async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>((resolve) => (release = resolve));
    let clientId = '';
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url: string, init: RequestInit) => {
        clientId = JSON.parse(String(init.body)).clientId;
        // The runtime's socket update comes before the answer, while the page still holds its local chip.
        status({ type: 'prompt-queue:update', tabId: 't1', items: [item('q1', 'hello', { clientId })] });
        expect(shown('t1')).toMatchObject([{ id: 'q1' }]);
        await gate;
        return new Response(JSON.stringify({ item: item('q1', 'hello', { clientId }), delivery: 'queued' }));
      }),
    );
    // A sync from another page's reconnect while the upload runs keeps the local chip.
    const sent = enqueuePrompt(undefined, 't1', 'hello', [], async () => {
      status({ type: 'prompt-queue:sync', queues: { t1: [item('q0', 'older')] } });
      expect(shown('t1')).toMatchObject([
        { id: 'q0' },
        { text: 'hello', clientId: expect.stringMatching(/^c-/) },
      ]);
      return { text: 'hello', images: [] };
    });
    await vi.waitFor(() => expect(clientId).not.toBe(''));
    release();
    await sent;
    expect(shown('t1')).toMatchObject([{ id: 'q1' }]);
  });

  it('is matched by its text when the runtime predates client ids', () => {
    const local = [
      item('local-1', 'same', { clientId: 'c-1' }),
      item('local-2', 'other', { clientId: 'c-2' }),
    ];
    expect(mergeQueue([item('q1', 'same')], local)).toMatchObject([{ id: 'q1' }, { id: 'local-2' }]);
    expect(mergeQueue([item('q1', 'same', { clientId: 'c-9' })], local)).toHaveLength(3);
    const server = [item('q1', 'x')];
    expect(mergeQueue(server, [])).toBe(server);
  });

  it('becomes a pending row in the timeline when the runtime later hands it to the agent', async () => {
    // Ids differ between tests: the hand-over tracker lives as long as the module.
    const queued = item('q-later', 'later', { clientId: 'c' });
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        status({ type: 'prompt-queue:update', tabId: 't1', items: [queued] });
        return new Response(JSON.stringify({ item: queued, delivery: 'queued' }));
      }),
    );
    const events: THandoffEvent[] = [];
    const off = onQueueHandoff('t1', (event) => events.push(event));
    await enqueuePrompt(undefined, 't1', 'later', [], async () => ({ text: 'later', images: [] }));
    expect(events).toEqual([]);
    // Given up on, it stays: no hand-over.
    status({
      type: 'prompt-queue:update',
      tabId: 't1',
      items: [{ ...queued, status: 'failed', reason: 'expired' }],
    });
    expect(events).toEqual([]);
    status({ type: 'prompt-queue:update', tabId: 't1', items: [] });
    off();
    expect(events).toMatchObject([{ kind: 'handoff', tabId: 't1', item: { id: 'q-later' } }]);
  });
});

describe('a refusal of the queue', () => {
  it('says the message is gone by its code, and by its words only without one', () => {
    expect(isGoneFromQueue(new QueueRequestError('Anything', 404, 'not-queued'))).toBe(true);
    expect(isGoneFromQueue(new QueueRequestError('Сообщения уже нет в очереди', 409, 'other'))).toBe(false);
    expect(isGoneFromQueue(new QueueRequestError('Сообщения уже нет в очереди', 409, null))).toBe(true);
    expect(isGoneFromQueue(new QueueRequestError('HTTP 500', 500, null))).toBe(false);
  });

  it('carries the code of the answer', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(JSON.stringify({ error: 'gone', code: 'not-queued' }), { status: 404 })),
    );
    const failure = await retryQueuedPrompt(undefined, 't1', 'q1').catch((error: unknown) => error);
    expect(failure).toMatchObject({ status: 404, code: 'not-queued', message: 'gone' });
    expect(isGoneFromQueue(failure)).toBe(true);
  });
});

describe('a message the runtime gave up on', () => {
  it('shows why, and is sent again or deleted from its row', async () => {
    const fetchMock = vi.fn(async () => new Response(JSON.stringify({ item: item('q1', 'x') })));
    vi.stubGlobal('fetch', fetchMock);
    status({
      type: 'prompt-queue:sync',
      queues: {
        t1: [item('q1', 'not delivered', { status: 'failed', reason: 'launch-failed' }), item('q2', 'waits')],
      },
    });
    render(<PromptQueueStack tabId="t1" onEdit={() => {}} />);
    expect(screen.getByText('Not sent: the agent did not start')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /Steer/ })).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: /Retry/ }));
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/platform/prompt-queue');
    expect(JSON.parse(String(init.body))).toEqual({ tabId: 't1', action: 'retry', id: 'q1' });
    await vi.waitFor(() =>
      expect(screen.getByRole('button', { name: 'Delete' })).toHaveProperty('disabled', false),
    );
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
    const [, removal] = fetchMock.mock.calls[1] as unknown as [string, RequestInit];
    expect(JSON.parse(String(removal.body))).toEqual({ tabId: 't1', action: 'remove', id: 'q1' });
  });

  it('says a message that never got typed or found the box taken', () => {
    status({
      type: 'prompt-queue:sync',
      queues: {
        t1: [
          item('q1', 'a', { status: 'failed', reason: 'not-delivered' }),
          item('q2', 'b', { status: 'failed', reason: 'input-occupied' }),
        ],
      },
    });
    render(<PromptQueueStack tabId="t1" onEdit={() => {}} />);
    expect(screen.getByText('Not sent: typing it into the agent kept failing')).toBeTruthy();
    expect(screen.getByText('Not sent: the agent input has someone else’s text')).toBeTruthy();
  });
});
