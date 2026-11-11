// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ConvId, Conversation, MachineId } from '@/next/protocol/v2';
import { noticeEventOf } from '@/next/features/notifications/notice-events';
import { chatStatusOf } from '@/next/features/sidebar/status';
import { CHAT_STATUS } from '@/next/features/sidebar/types';
import { mergeLog } from '@/next/stores/notif-log';

const markSeen = vi.fn();
vi.mock('@/next/commands', () => ({ markSeen: (...args: unknown[]) => markSeen(...args) }));

const { MARK_AGAIN_MS, keepMarking, readUpToOf, useReadWhileVisible } =
  await import('@/next/bindings/read-while-visible');

const M = 'm-1' as MachineId;
const ID = 'tab-1' as ConvId;

const chat = (endedAt: number | null, seenAt: number): Conversation =>
  ({
    id: ID,
    machineId: M,
    title: 'chat',
    rev: 1,
    lastActivityAt: endedAt ?? 0,
    seenAt,
    state: { k: 'idle', since: 0 },
    gate: null,
    lastTurn: endedAt === null ? null : { id: 't1', status: 'done', endedAt, startedAt: 0, snippet: 'ответ' },
  }) as unknown as Conversation;

let visibility: DocumentVisibilityState = 'visible';

beforeEach(() => {
  vi.useFakeTimers();
  markSeen.mockReset();
  visibility = 'visible';
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
});
afterEach(() => vi.useRealTimers());

const show = (state: DocumentVisibilityState) =>
  act(() => {
    visibility = state;
    document.dispatchEvent(new Event('visibilitychange'));
  });

describe('a chat becomes read', () => {
  it('a new answer in a chat nobody looks at is unread; read up to its end by the machine clock', () => {
    expect(chatStatusOf(chat(200, 100), true)).toBe(CHAT_STATUS.done);
    expect(readUpToOf(chat(200, 100))).toBe(200);
    expect(chatStatusOf(chat(200, 200), true)).toBe(CHAT_STATUS.none);
    expect(readUpToOf(chat(200, 200))).toBeNull();
    expect(readUpToOf(chat(null, 0))).toBeNull();
  });

  it('opened and seen: marked read at once, and again until the machine says it is', () => {
    const view = renderHook(({ conv }) => useReadWhileVisible(conv), {
      initialProps: { conv: chat(200, 100) },
    });
    expect(markSeen).toHaveBeenCalledTimes(1);
    expect(markSeen).toHaveBeenLastCalledWith(M, ID, 200);
    // The command was lost (a busy machine timed it out): no word came, the mark goes again.
    act(() => vi.advanceTimersByTime(MARK_AGAIN_MS[0]));
    expect(markSeen).toHaveBeenCalledTimes(2);
    // conversation_seen came: nothing more is sent.
    view.rerender({ conv: chat(200, 200) });
    act(() => vi.advanceTimersByTime(60_000));
    expect(markSeen).toHaveBeenCalledTimes(2);
  });

  it('open in a hidden tab: read only when the tab comes to the front', () => {
    visibility = 'hidden';
    renderHook(() => useReadWhileVisible(chat(200, 100)));
    expect(markSeen).not.toHaveBeenCalled();
    show('visible');
    expect(markSeen).toHaveBeenCalledWith(M, ID, 200);
    show('hidden');
    act(() => vi.advanceTimersByTime(60_000));
    expect(markSeen).toHaveBeenCalledTimes(1);
  });

  it('the waits between marks grow and the last one repeats', () => {
    const send = vi.fn();
    const stop = keepMarking(send);
    let at = 0;
    for (const [i, wait] of [...MARK_AGAIN_MS, MARK_AGAIN_MS.at(-1)!].entries()) {
      vi.advanceTimersByTime(wait - 1);
      expect(send).toHaveBeenCalledTimes(i + 1);
      vi.advanceTimersByTime(1);
      at += wait;
    }
    expect(at).toBeGreaterThan(0);
    stop();
    vi.advanceTimersByTime(120_000);
    expect(send).toHaveBeenCalledTimes(MARK_AGAIN_MS.length + 2);
  });

  it("the notification goes read with the chat: the log's row of the turn is read once the chat is", () => {
    const unread = chat(200, 100);
    const event = noticeEventOf(unread, true);
    expect(event).not.toBeNull();
    const logged = {
      key: event!.key,
      chatId: ID,
      kind: 'done' as const,
      icon: 'x',
      title: 'chat',
      text: 'ответ',
      at: 200,
    };
    const log = mergeLog([], [logged], new Map([[ID, event!.key]]), new Set());
    expect(log[0].read).toBe(false);
    const read = chat(200, 200);
    const after = mergeLog(log, [], new Map([[ID, noticeEventOf(read, true)?.key ?? null]]), new Set());
    expect(after[0].read).toBe(true);
  });
});
