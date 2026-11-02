// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const setConversationApps = vi.fn();
let record: { apps?: string[] } | undefined;
const toast = vi.fn();

vi.mock('@/next/commands', () => ({ setConversationApps: (...a: unknown[]) => setConversationApps(...a) }));
vi.mock('@/next/stores/conversations', () => ({ useConversation: () => record }));
vi.mock('@/next/ui/toast', () => ({ toast: (...a: unknown[]) => toast(...a) }));

const { useChatApps } = await import('@/next/bindings/use-chat-apps');

const handle = (done: Promise<unknown>) => ({ opId: 'op', done });

beforeEach(() => {
  record = { apps: ['a.one'] };
  setConversationApps.mockReset();
  toast.mockReset();
});
afterEach(cleanup);

describe('useChatApps', () => {
  it('sends the whole list and changes nothing on screen until the machine says so', async () => {
    let finish: (v: unknown) => void = () => undefined;
    setConversationApps.mockReturnValue(handle(new Promise((r) => (finish = r))));
    const { result } = renderHook(() => useChatApps('m1' as never, 'tab-1' as never));
    let turning: Promise<void> = Promise.resolve();
    act(() => {
      turning = result.current.turn('b.two', true);
    });
    expect(setConversationApps).toHaveBeenCalledWith('m1', 'tab-1', ['a.one', 'b.two']);
    // The command is on its way: the row is the chat's record, which has not changed, and the change is busy.
    expect(result.current.on).toEqual(['a.one']);
    expect(result.current.busy).toBe('b.two');
    await act(async () => {
      finish({ k: 'done' });
      await turning;
    });
    expect(result.current.busy).toBeNull();
    // Done on the machine, but the record is the socket's word: it still reads as it did.
    expect(result.current.on).toEqual(['a.one']);
  });

  it('takes one change at a time', async () => {
    setConversationApps.mockReturnValue(handle(new Promise(() => undefined)));
    const { result } = renderHook(() => useChatApps('m1' as never, 'tab-1' as never));
    act(() => void result.current.turn('b.two', true));
    act(() => void result.current.turn('c.three', true));
    expect(setConversationApps).toHaveBeenCalledTimes(1);
  });

  it('tells a refusal in words and leaves the apps as they were', async () => {
    setConversationApps.mockReturnValue(
      handle(Promise.resolve({ k: 'failed', failure: { r: 'refused', code: 'not-found' } })),
    );
    const { result } = renderHook(() => useChatApps('m1' as never, 'tab-1' as never));
    await act(async () => {
      await result.current.turn('a.one', false);
    });
    expect(toast).toHaveBeenCalledTimes(1);
    expect(toast.mock.calls[0]?.[1]).toMatchObject({ tone: 'error' });
    expect(result.current.on).toEqual(['a.one']);
    expect(result.current.busy).toBeNull();
  });

  it('does nothing without a chat', async () => {
    const { result } = renderHook(() => useChatApps('m1' as never, null));
    await act(async () => {
      await result.current.turn('a.one', true);
    });
    expect(setConversationApps).not.toHaveBeenCalled();
  });
});
