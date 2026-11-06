import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { agentRows, agentStatusOf, chatTarget, watchAgents } from '@/next/features/app-views/agents';
import { setScheduler } from '@/next/core/store';
import type { Conversation, MachineId } from '@/next/protocol/v2';
import { conversationsStore } from '@/next/stores/conversations';
import { machinesStore, type MachineView } from '@/next/stores/machines';

const M = 'm1' as MachineId;

const conv = (id: string, over: Record<string, unknown> = {}) =>
  ({
    id,
    machineId: M,
    title: `Chat ${id}`,
    provider: 'claude',
    cwd: '/secret/dir',
    pinned: false,
    lastActivityAt: 1,
    state: { k: 'ready', since: 1 },
    gate: null,
    lastTurn: null,
    seenAt: 0,
    prompt: 'the private prompt',
    ...over,
  }) as unknown as Conversation;

beforeEach(() => {
  setScheduler('sync');
  machinesStore.set(M, { machine: { id: M, name: 'Моя ВМ' } } as unknown as MachineView);
});
afterEach(() => {
  for (const k of [...conversationsStore.keys()]) conversationsStore.delete(k);
  machinesStore.delete(M);
  setScheduler('frame');
});

describe('agents for a view', () => {
  it('names the status the sidebar shows, in three words', () => {
    expect(agentStatusOf(conv('tab-a', { state: { k: 'working', since: 1 } }))).toBe('working');
    expect(agentStatusOf(conv('tab-a', { state: { k: 'waiting-question', since: 1 } }))).toBe('waiting');
    expect(
      agentStatusOf(conv('tab-a', { state: { k: 'waiting-permission', since: 1 }, gate: { kind: 'plan' } })),
    ).toBe('waiting');
    expect(agentStatusOf(conv('tab-a'))).toBe('free');
  });

  it("lists the machine's conversations with no text, prompt or path", () => {
    conversationsStore.set('tab-a', conv('tab-a', { lastActivityAt: 1 }));
    conversationsStore.set('tab-b', conv('tab-b', { lastActivityAt: 2, state: { k: 'working', since: 1 } }));
    conversationsStore.set('tab-c', conv('tab-c', { machineId: 'other' }));
    const rows = agentRows(M);
    expect(rows).toEqual([
      { id: 'tab-b', title: 'Chat tab-b', status: 'working', provider: 'claude', machine: 'Моя ВМ' },
      { id: 'tab-a', title: 'Chat tab-a', status: 'free', provider: 'claude', machine: 'Моя ВМ' },
    ]);
    expect(JSON.stringify(rows)).not.toMatch(/secret|private/);
  });

  it('tells a change of status or list, and nothing else', () => {
    conversationsStore.set('tab-a', conv('tab-a'));
    const told = vi.fn();
    const stop = watchAgents(M, told);
    conversationsStore.set('tab-a', conv('tab-a', { seenAt: 5 }));
    expect(told).not.toHaveBeenCalled();
    conversationsStore.set('tab-a', conv('tab-a', { state: { k: 'working', since: 2 } }));
    expect(told).toHaveBeenCalledTimes(1);
    expect(told.mock.calls[0]?.[0][0].status).toBe('working');
    conversationsStore.set('tab-b', conv('tab-b'));
    expect(told).toHaveBeenCalledTimes(2);
    stop();
    conversationsStore.delete('tab-b');
    expect(told).toHaveBeenCalledTimes(2);
  });

  it('reads chat:<id> and nothing else as a conversation', () => {
    expect(chatTarget('chat:tab-abc_1')).toBe('tab-abc_1');
    expect(chatTarget('chat:abc')).toBeNull();
    expect(chatTarget('app:a.b')).toBeNull();
    expect(chatTarget('chat:tab-a/../b')).toBeNull();
  });
});
