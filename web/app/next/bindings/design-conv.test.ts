// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const create = vi.hoisted(() => vi.fn());
vi.mock('@/next/commands', () => ({ createConversation: create }));
const goTo = vi.hoisted(() => vi.fn());
vi.mock('@/next/routes/routes', async (original) => ({
  ...(await original<typeof import('@/next/routes/routes')>()),
  goTo,
}));

import {
  bindDesignConv,
  creatingStore,
  designBindingStore,
  designSeenStore,
  resetDesignConvs,
  startDesignConv,
  useDesignConv,
} from '@/next/bindings/design-conv';
import { setScheduler } from '@/next/core/store';
import { DESIGN_CONV, designKey } from '@/next/features/designly/design-conv';
import type { Conversation, MachineId } from '@/next/protocol/v2';
import { conversationsStore, convsLoadedStore } from '@/next/stores/conversations';

const M = 'm-design' as MachineId;
const DIR = '/lib/abc';
const KEY = designKey(M, 'abc');
const STORAGE = 'hx-next-design-convs';

const conv = (id: string, lastActivityAt: number, cwd = DIR) =>
  ({ id, machineId: M, cwd, lastActivityAt, pinned: false }) as unknown as Conversation;

const putList = (list: Conversation[]) =>
  act(() => {
    for (const c of list) conversationsStore.set(c.id as Conversation['id'], c);
    convsLoadedStore.set(M, true);
  });

/** A create that answers when the test says so. */
const pendingCreate = () => {
  let finish: (id: string | null) => void = () => {};
  const conversation = new Promise<string | null>((resolve) => {
    finish = resolve;
  });
  const done = conversation.then((id) =>
    id ? { k: 'done' } : { k: 'failed', failure: { r: 'refused', code: 'internal' } },
  );
  create.mockReturnValueOnce({ firstClientId: 'c-1', conversation, done });
  return finish;
};

beforeEach(() => {
  setScheduler('sync');
  localStorage.clear();
  resetDesignConvs();
  create.mockReset();
});
afterEach(() => {
  cleanup();
  for (const k of [...conversationsStore.keys()]) conversationsStore.delete(k);
  convsLoadedStore.delete(M);
  setScheduler('frame');
});

describe("the design's current chat", () => {
  it('shows the chat the person came from and remembers it as the last one', () => {
    bindDesignConv(KEY, { id: 'tab-last', fresh: false });
    putList([conv('tab-last', 10), conv('tab-from', 20, '/elsewhere')]);
    const { result } = renderHook(() => useDesignConv(M, 'abc', 'tab-from'));
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-from' });
    expect(designBindingStore.get(KEY)).toEqual({ id: 'tab-from', fresh: false });
    expect(designSeenStore.get(KEY)).toEqual(['tab-from', 'tab-last']);
  });

  it('opens on the last chat the browser kept, before the machine has answered', () => {
    localStorage.setItem(STORAGE, JSON.stringify({ [KEY]: { last: 'tab-kept', seen: ['tab-kept'] } }));
    const { result } = renderHook(() => useDesignConv(M, 'abc', null));
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-kept' });
  });

  it('reads the bare id an older page kept', () => {
    localStorage.setItem(STORAGE, JSON.stringify({ [KEY]: 'tab-old' }));
    const { result } = renderHook(() => useDesignConv(M, 'abc', null));
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-old' });
  });

  it("is a new chat without one, not the folder's latest", () => {
    putList([conv('tab-a', 30)]);
    const { result } = renderHook(() => useDesignConv(M, 'abc', null));
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.none });
  });

  it('does not switch when the list is read again or another chat of the folder works', () => {
    bindDesignConv(KEY, { id: 'tab-a', fresh: false });
    putList([conv('tab-a', 30), conv('tab-b', 10)]);
    const { result } = renderHook(() => useDesignConv(M, 'abc', null));
    putList([conv('tab-b', 99), conv('tab-c', 98)]);
    putList([conv('tab-a', 31)]);
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-a' });
  });

  it('changes on a pick and keeps it, over the chat the address came from', () => {
    putList([conv('tab-from', 20), conv('tab-b', 10)]);
    const { result, rerender } = renderHook(() => useDesignConv(M, 'abc', 'tab-from'));
    act(() => result.current.pick('tab-b'));
    rerender();
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-b' });
    expect(JSON.parse(localStorage.getItem(STORAGE) ?? '{}')[KEY].last).toBe('tab-b');
    act(() => result.current.pick(null));
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.none });
  });

  it('takes ?chat= off the address on a pick, so a reload does not bring the old chat back', () => {
    putList([conv('tab-from', 20)]);
    const { result } = renderHook(() => useDesignConv(M, 'abc', 'tab-from'));
    act(() => result.current.pick(null));
    expect(goTo).toHaveBeenCalledWith({ kind: 'site', id: 'abc' }, { replace: true });
  });

  it('leaves the address alone when it names no chat', () => {
    putList([conv('tab-b', 10)]);
    goTo.mockClear();
    const { result } = renderHook(() => useDesignConv(M, 'abc', null));
    act(() => result.current.pick('tab-b'));
    expect(goTo).not.toHaveBeenCalled();
  });
});

describe("the design's first message", () => {
  it('makes exactly one chat, however many times it is sent, and shows it at once', async () => {
    putList([]);
    const finish = pendingCreate();
    const first = {
      machine: M,
      key: KEY,
      dir: DIR,
      choice: null,
      text: 'hi',
      attachments: [],
      onRefused: vi.fn(),
    };
    expect(startDesignConv(first)).toBe('c-1');
    expect(startDesignConv(first)).toBeNull();
    expect(create).toHaveBeenCalledTimes(1);
    expect(create.mock.calls[0][1]).toMatchObject({ where: { cwd: DIR }, first: { text: 'hi' } });

    const { result } = renderHook(() => useDesignConv(M, 'abc', null));
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.none });
    await act(async () => finish('tab-new'));
    expect(creatingStore.get(KEY)).toEqual({ clientId: 'c-1', conv: 'tab-new' });
    // The machine's list has not got it yet: still this chat, not a new one and not another one.
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-new' });
    putList([conv('tab-old', 1), conv('tab-new', 2)]);
    expect(result.current.picked).toEqual({ k: DESIGN_CONV.conv, id: 'tab-new' });
    expect(designBindingStore.get(KEY)).toEqual({ id: 'tab-new', fresh: false });
  });

  it('gives the words back and binds nothing when the machine refuses the chat', async () => {
    const finish = pendingCreate();
    const onRefused = vi.fn();
    startDesignConv({ machine: M, key: KEY, dir: DIR, choice: null, text: 'hi', attachments: [], onRefused });
    await act(async () => finish(null));
    expect(onRefused).toHaveBeenCalledWith('hi');
    expect(creatingStore.has(KEY)).toBe(false);
    expect(designBindingStore.has(KEY)).toBe(false);
  });
});
