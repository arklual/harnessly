import { beforeEach, describe, expect, it, vi } from 'vitest';

const page = vi.hoisted(() => ({ getPage: vi.fn() }));
vi.mock('@/next/core/net/http', () => ({ getPage: page.getPage }));
vi.mock('@/next/core/net/machine', () => ({ gatewayPath: (p: string) => `/agents/api/x${p}` }));

import { asOffline, centralStore, isMachineOff, movedKey, refreshCentral } from '@/next/stores/central';
import { connectionStore } from '@/next/stores/connection';
import { conversationsStore, convsCachedStore } from '@/next/stores/conversations';
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
    centralStore.set({ enabled: false, off: new Set(), moved: new Set(), names: new Map(), on: new Set() });
    for (const id of [...conversationsStore.keys()]) conversationsStore.delete(id);
    for (const m of [...connectionStore.keys()]) connectionStore.delete(m);
    for (const m of [...convsCachedStore.keys()]) convsCachedStore.delete(m);
  });

  it('shows a chat of an off machine as stopped and not resumable', () => {
    const c = asOffline(chat('tab-1', 'a', 5));
    expect(c.state).toEqual({ k: 'stopped', since: 5, resumable: false });
    expect(c.queue.count).toBe(0);
  });

  it('puts only the chats of off machines into the list', async () => {
    answer(true, ['a'], [chat('tab-1', 'a', 5), chat('tab-2', 'b', 6)]);
    await refreshCentral();
    expect(conversationsStore.get('tab-1')?.state.k).toBe('stopped');
    expect(conversationsStore.get('tab-2')).toBeUndefined();
    expect(convsCachedStore.has('a')).toBe(true);
    expect(isMachineOff('a')).toBe(true);
    expect(isMachineOff('b')).toBe(false);
  });

  it('leaves a machine with a live link to itself', async () => {
    connectionStore.set('a', { k: 'live' } as never);
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
});
