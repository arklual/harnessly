// @vitest-environment jsdom
import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { machineSwitchLeavesChat, useShellModel, useTabChats } from '@/next/bindings/shell-model';
import { stripTabOf } from '@/next/bindings/tabs';
import { resetCore } from '@/next/core/testing/reset';
import { PACE } from '@/next/features/shell/tab-strip';
import { CHAT_STATUS } from '@/next/features/sidebar/types';
import type { Conversation, ConvId, MachineId } from '@/next/protocol/v2';
import { connectionStore } from '@/next/stores/connection';
import { conversationsStore, convsLoadedStore } from '@/next/stores/conversations';
import { activeMachineStore } from '@/next/stores/machines';
import { createTab } from '@/next/stores/tabs';

const A = 'vm-a' as MachineId;
const B = 'vm-b' as MachineId;

const conv = (id: string, machineId: MachineId, state: Conversation['state']): Conversation =>
  ({
    id,
    machineId,
    title: `chat ${id}`,
    state,
    gate: null,
    lastTurn: null,
    seenAt: 0,
    pinned: false,
    projectId: null,
    createdAt: 1,
    lastPromptAt: 1,
    lastActivityAt: 1,
    rev: 1,
  }) as unknown as Conversation;

const put = (...list: Conversation[]) =>
  act(() => {
    for (const c of list) conversationsStore.set(c.id as ConvId, c);
    convsLoadedStore.set(A, true);
    convsLoadedStore.set(B, true);
  });

afterEach(() => {
  cleanup();
  resetCore();
});

describe('the tabs over the window know the chats of every machine', () => {
  it('keeps the status of a chat of machine A on its tab while machine B is the open one', () => {
    put(
      conv('on-a', A, { k: 'working', since: 1, turnId: 't' } as Conversation['state']),
      conv('on-b', B, { k: 'idle' } as Conversation['state']),
    );
    act(() => activeMachineStore.set(B));
    const { result } = renderHook(() => useTabChats());
    const tab = stripTabOf(createTab('/c/on-a'), result.current);
    expect(tab.title).toBe('chat on-a');
    expect(tab.busy).toBe(PACE.working);
  });

  it('says the same status for a chat as its row in the sidebar', () => {
    put(conv('q', A, { k: 'waiting-question' } as Conversation['state']));
    act(() => activeMachineStore.set(A));
    const { result } = renderHook(() => ({ tabs: useTabChats(), shell: useShellModel() }));
    const row = result.current.shell.chats?.find((c) => c.id === 'q');
    expect(row?.status).toBe(CHAT_STATUS.question);
    expect(result.current.tabs.get('q')?.status).toBe(row?.status);
  });

  it("reads each machine's own link: a chat on a machine that is away waits for it", () => {
    put(conv('w', A, { k: 'working', since: 1, turnId: 't' } as Conversation['state']));
    act(() => {
      activeMachineStore.set(B);
      connectionStore.set(A, { k: 'offline', cause: 'network', retryAt: Infinity } as never);
      connectionStore.set(B, { k: 'live' } as never);
    });
    const { result } = renderHook(() => useTabChats());
    expect(result.current.get('w')?.status).toBe(CHAT_STATUS.waitingMachine);
  });
});

describe('picking another machine', () => {
  it('leaves a chat of the machine that was open for that machine’s start page', () => {
    expect(machineSwitchLeavesChat(A, B)).toBe(true);
  });

  it('keeps the screen when the chat on it is of the machine picked, or no chat is on it', () => {
    expect(machineSwitchLeavesChat(B, B)).toBe(false);
    expect(machineSwitchLeavesChat(null, B)).toBe(false);
  });
});
