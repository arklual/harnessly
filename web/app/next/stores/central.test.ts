import { beforeEach, describe, expect, it, vi } from 'vitest';

const page = vi.hoisted(() => ({ getPage: vi.fn() }));
// The list is asked by a conditional GET; the fake answers both kinds of request.
vi.mock('@/next/core/net/http', () => ({ getPage: page.getPage, getPageIfChanged: page.getPage }));
vi.mock('@/next/core/net/machine', () => ({ gatewayPath: (p: string) => `/agents/api/x${p}` }));

import {
  asOffline,
  centralStore,
  forgetCentralList,
  isMachineOff,
  keepsHistory,
  movedKey,
  refreshCentral,
  resetCentral,
} from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';
import { conversationsStore, convsCachedStore, convsLoadedStore } from '@/next/stores/conversations';
import {
  fromLive,
  fromSnapshot,
  mergeConversation,
  originOf,
  SOURCE,
} from '@/next/stores/conversation-merge';
import type { Conversation } from '@/next/protocol/v2';

const chat = (id: string, machineId: string, at: number) =>
  ({
    id,
    machineId,
    title: id,
    lastActivityAt: at,
    state: { k: 'idle' },
    queue: { count: 2, paused: null },
    gate: null,
  }) as unknown as Conversation;

const answer = (enabled: boolean, off: string[], conversations: Conversation[]) =>
  page.getPage.mockResolvedValue({
    ok: true,
    value: {
      enabled,
      machines: ['a', 'b'].map((id) => ({ id, name: id, state: off.includes(id) ? 'offline' : 'online' })),
      conversations,
    },
  });

describe('central history', () => {
  beforeEach(() => {
    page.getPage.mockReset();
    forgetCentralList();
    centralStore.set({ enabled: false, off: new Set(), moved: new Set(), names: new Map(), on: new Set() });
    for (const id of [...conversationsStore.keys()]) conversationsStore.delete(id);
    for (const m of [...connectionStore.keys()]) connectionStore.delete(m);
    for (const m of [...convsCachedStore.keys()]) convsCachedStore.delete(m);
    for (const m of [...convsLoadedStore.keys()]) convsLoadedStore.delete(m);
  });

  it('names the machines the person administers but keeps their chats out of the list (#57)', async () => {
    page.getPage.mockResolvedValue({
      ok: true,
      value: {
        enabled: true,
        machines: [{ id: 'mine', name: 'Mine', state: 'online' }],
        others: [{ id: 'theirs', name: 'Theirs', state: 'offline' }],
        conversations: [chat('tab-1', 'mine', 5)],
      },
    });
    await refreshCentral();
    const c = centralStore.get();
    expect([...c.on]).toEqual(['mine']);
    expect(c.names.get('theirs')).toBe('Theirs');
    expect(isMachineOff('theirs')).toBe(true);
    expect(keepsHistory('theirs')).toBe(false);
  });

  it('shows a chat of an off machine as stopped and not resumable', () => {
    const c = asOffline(chat('tab-1', 'a', 5));
    expect(c.state).toEqual({ k: 'stopped', since: 5, resumable: false });
    expect(c.queue.count).toBe(0);
  });

  it('puts the chats of off machines into the list, not those of a machine whose own list is here', async () => {
    connectionStore.set('b', { k: 'live' } as never);
    convsLoadedStore.set('b', true);
    answer(true, ['a'], [chat('tab-1', 'a', 5), chat('tab-2', 'b', 6)]);
    await refreshCentral();
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');
    expect(conversationsStore.get('tab-2')).toBeUndefined();
    expect(convsCachedStore.has('a')).toBe(true);
    expect(isMachineOff('a')).toBe(true);
    expect(isMachineOff('b')).toBe(false);
  });

  it('draws a machine the cluster still calls online from the copy until its own list comes (HRN-902)', async () => {
    connectionStore.set('b', { k: 'connecting' } as never);
    answer(true, [], [chat('tab-2', 'b', 6)]);
    await refreshCentral();
    // As the copy has it, not stopped: the machine may only be starting.
    expect(conversationsStore.get('tab-2')?.state.k).toBe('idle');
    expect(convsCachedStore.has('b')).toBe(true);
    expect(isMachineOff('b')).toBe(false);
  });

  it('draws at rev 0, so the machine word replaces the copy whatever rev it kept', async () => {
    connectionStore.set('b', { k: 'connecting' } as never);
    answer(
      true,
      [],
      [{ ...chat('tab-2', 'b', 6), rev: 99, state: { k: 'working' } } as unknown as Conversation],
    );
    await refreshCentral();
    expect(conversationsStore.get('tab-2')?.rev).toBe(0);
    expect(conversationsStore.get('tab-2')?.state.k).toBe('working');
  });

  it('does not lay a copy with the same time over what the machine said', async () => {
    // The machine's link is not live yet, so its chats are still drawn from the copy.
    connectionStore.set('b', { k: 'connecting' } as never);
    const live = mergeConversation(
      { ...chat('tab-2', 'b', 6), rev: 4, state: { k: 'working' } } as unknown as Conversation,
      fromLive('e1'),
    );
    answer(true, [], [chat('tab-2', 'b', 6)]);
    await refreshCentral();
    expect(conversationsStore.get('tab-2')).toBe(live);
    // A copy that knows of later activity is taken.
    answer(true, [], [chat('tab-2', 'b', 7)]);
    await refreshCentral();
    expect(conversationsStore.get('tab-2')?.lastActivityAt).toBe(7);
    expect(originOf(conversationsStore.get('tab-2')!).src).toBe(SOURCE.central);
  });

  it('draws a chat of an off machine stopped but keeps what the machine said of it', async () => {
    mergeConversation(
      {
        ...chat('tab-1', 'a', 5),
        rev: 4,
        title: 'renamed live',
        state: { k: 'working' },
      } as unknown as Conversation,
      fromLive('e1'),
    );
    answer(true, ['a'], [chat('tab-1', 'a', 5)]);
    await refreshCentral();
    const shown = conversationsStore.get('tab-1')!;
    expect(shown.title).toBe('renamed live');
    expect(shown.state).toEqual({ k: 'stopped', since: 5, resumable: false });
    // The stopped look is the page's: the machine's list replaces it when the machine is back, at the same rev.
    expect(originOf(shown)).toEqual({ src: SOURCE.live, epoch: 'e1', veiled: true });
    mergeConversation(
      { ...chat('tab-9', 'a', 5), id: 'tab-1', rev: 4, title: 'machine is back' } as unknown as Conversation,
      fromSnapshot('e1'),
    );
    expect(conversationsStore.get('tab-1')?.state.k).toBe('idle');
    answer(true, ['a'], [chat('tab-1', 'a', 5)]);
    await refreshCentral();
    // The same list merged anew changes nothing.
    await refreshCentral(true);
    expect(conversationsStore.get('tab-1')).toMatchObject({
      title: 'machine is back',
      state: { k: 'stopped' },
    });
  });

  it('drops the answer to a request that is no longer the last one', async () => {
    connectionStore.set('b', { k: 'connecting' } as never);
    const list = (conversations: Conversation[]) => ({
      ok: true,
      value: { enabled: true, machines: [{ id: 'b', name: 'b', state: 'online' }], conversations },
    });
    let answerFirst: (v: unknown) => void = () => {};
    page.getPage.mockReturnValueOnce(new Promise((resolve) => (answerFirst = resolve)));
    page.getPage.mockResolvedValueOnce(list([chat('tab-new', 'b', 9)]));
    const first = refreshCentral();
    await refreshCentral();
    expect(conversationsStore.has('tab-new')).toBe(true);
    // The earlier request answers late, with the list as it was before.
    answerFirst(list([chat('tab-old', 'b', 3), { ...chat('tab-new', 'b', 9), title: 'stale' }]));
    await first;
    expect(conversationsStore.has('tab-old')).toBe(false);
    expect(conversationsStore.get('tab-new')?.title).toBe('tab-new');
  });

  it('merges anew when the request that asked for it was overtaken', async () => {
    page.getPage.mockResolvedValue({
      ok: true,
      tag: 'W/"v1"',
      value: {
        enabled: true,
        machines: [{ id: 'a', name: 'a', state: 'offline' }],
        conversations: [chat('tab-1', 'a', 5)],
      },
    });
    await refreshCentral();
    conversationsStore.delete('tab-1');
    let answerFirst: (v: unknown) => void = () => {};
    page.getPage.mockReturnValueOnce(new Promise((resolve) => (answerFirst = resolve)));
    page.getPage.mockResolvedValueOnce({ same: true });
    const first = refreshCentral(true);
    await refreshCentral();
    answerFirst({ same: true });
    await first;
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');
  });

  it('leaves a machine with a live link to itself', async () => {
    connectionStore.set('a', { k: 'live' } as never);
    convsLoadedStore.set('a', true);
    answer(true, ['a'], [chat('tab-1', 'a', 5)]);
    await refreshCentral();
    expect(conversationsStore.get('tab-1')).toBeUndefined();
    expect(isMachineOff('a')).toBe(false);
  });

  it('does nothing while the copy is off for the person', async () => {
    answer(false, ['a'], [chat('tab-1', 'a', 5)]);
    await refreshCentral();
    expect(conversationsStore.get('tab-1')).toBeUndefined();
    expect(isMachineOff('a')).toBe(false);
  });

  it('stays on when one answer says off, so the sidebar does not swap and swap back (HRN-1167)', async () => {
    answer(true, [], []);
    await refreshCentral();
    page.getPage.mockResolvedValue({ ok: true, value: { enabled: false, machines: [], conversations: [] } });
    await refreshCentral();
    expect(centralStore.get().enabled).toBe(true);
    expect([...centralStore.get().on].sort()).toEqual(['a', 'b']);
  });

  it('starts off again in a new session (HRN-1167)', async () => {
    answer(true, [], []);
    await refreshCentral();
    resetCentral();
    answer(false, [], []);
    await refreshCentral();
    expect(centralStore.get().enabled).toBe(false);
  });

  it('knows the chats continued elsewhere and the machines by name', async () => {
    page.getPage.mockResolvedValue({
      ok: true,
      value: {
        enabled: true,
        machines: [{ id: 'a', name: 'Ромина ВМ', state: 'offline' }],
        conversations: [],
        moved: [{ machineId: 'a', convId: 'tab-1', toMachineId: 'b', toConvId: 'tab-9' }],
      },
    });
    await refreshCentral();
    expect(centralStore.get().moved.has(movedKey('a', 'tab-1'))).toBe(true);
    expect(centralStore.get().names.get('a')).toBe('Ромина ВМ');
    expect([...centralStore.get().on]).toEqual(['a']);
  });

  it('reads an older gateway without the moves', async () => {
    answer(true, [], []);
    await refreshCentral();
    expect(centralStore.get().moved.size).toBe(0);
  });

  it('keeps the page state when the gateway fails', async () => {
    page.getPage.mockResolvedValue({ r: 'failed', code: 'network', status: null });
    await refreshCentral();
    expect(centralStore.get().enabled).toBe(false);
  });

  it('reads the history of every machine the copy keeps, on or off (HRN-879)', async () => {
    connectionStore.set('a', { k: 'stale' } as never);
    connectionStore.set('b', { k: 'live' } as never);
    answer(true, [], []);
    await refreshCentral();
    // The cluster still calls both online: the copy is read all the same, the machine only for what it lacks.
    expect(keepsHistory('a')).toBe(true);
    expect(keepsHistory('b')).toBe(true);
    expect(keepsHistory('c')).toBe(false);
    expect(keepsHistory(null)).toBe(false);
    expect(isMachineOff('a')).toBe(false);
  });

  it('reads nothing from the copy while it is off for the person', async () => {
    answer(false, [], []);
    await refreshCentral();
    expect(keepsHistory('a')).toBe(false);
  });

  it('names the list it holds and applies nothing when the cluster says it has not changed', async () => {
    page.getPage.mockResolvedValue({
      ok: true,
      tag: 'W/"v1"',
      value: {
        enabled: true,
        machines: [{ id: 'a', name: 'a', state: 'offline' }],
        conversations: [chat('tab-1', 'a', 5)],
      },
    });
    await refreshCentral();
    expect(page.getPage.mock.calls[0]?.[3]).toBeNull();
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');

    // The page changes the chat; an unchanged list does not put the copy back over it.
    conversationsStore.delete('tab-1');
    page.getPage.mockResolvedValue({ same: true });
    await refreshCentral();
    expect(page.getPage.mock.calls[1]?.[3]).toBe('W/"v1"');
    expect(conversationsStore.has('tab-1')).toBe(false);

    // A link that changed asks for the held list to be merged anew.
    await refreshCentral(true);
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');
  });

  it('asks for the whole list and draws it again once the held one is forgotten', async () => {
    const value = {
      enabled: true,
      machines: [{ id: 'a', name: 'a', state: 'offline' }],
      conversations: [chat('tab-1', 'a', 5)],
    };
    page.getPage.mockResolvedValue({ ok: true, tag: 'W/"v1"', value });
    await refreshCentral();
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');

    // me names the person on a device that kept nothing of them: the stores are emptied, the held list forgotten.
    conversationsStore.delete('tab-1');
    convsCachedStore.delete('a');
    forgetCentralList();
    await refreshCentral();
    // No tag is named, so the cluster answers with the list itself, and it is drawn.
    expect(page.getPage.mock.calls[1]?.[3]).toBeNull();
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');
    expect(convsCachedStore.has('a')).toBe(true);
  });

  it('asks for the whole list again after an answer without a tag', async () => {
    const value = { enabled: true, machines: [], conversations: [] };
    page.getPage.mockResolvedValue({ ok: true, tag: 'W/"v1"', value });
    await refreshCentral();
    page.getPage.mockResolvedValue({ ok: true, tag: null, value });
    await refreshCentral();
    await refreshCentral();
    expect(page.getPage.mock.calls.map((c) => c[3])).toEqual([null, 'W/"v1"', null]);
  });
});
