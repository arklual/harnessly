import { describe, expect, it } from 'vitest';
import { queuedViewOf } from '@/next/bindings/queued-chat';
import type { QueuedChat } from '@/next/commands/queued-chats';
import { queuedRows } from '@/next/features/queued-chat/rows';
import { QUEUED_VIEW } from '@/next/features/queued-chat/queued-chat';
import { CHAT_STATUS } from '@/next/features/sidebar/types';

const item = (over: Partial<QueuedChat> = {}): QueuedChat => ({
  id: 'q1',
  machineId: 'm1',
  clientId: 'c1',
  text: 'сделай отчёт\nи пришли ссылку',
  state: 'queued',
  error: null,
  convId: null,
  createdAt: 10,
  ...over,
});

// a new chat that waits in the hub for its machine is a chat of the sidebar, by its first line.
describe('a chat that waits for its machine', () => {
  it('stands in the sidebar by its first line, waiting for its machine, or failed', () => {
    const rows = queuedRows([item(), item({ id: 'q2', state: 'failed', error: 'нет' })], () => 'runner-2');
    expect(rows).toEqual([
      expect.objectContaining({
        id: 'queued:q1',
        title: 'сделай отчёт',
        status: CHAT_STATUS.waitingMachine,
        machine: 'runner-2',
        lastPromptAt: 10,
      }),
      expect.objectContaining({ id: 'queued:q2', status: CHAT_STATUS.failed }),
    ]);
  });

  it('leaves the sidebar once the machine made it: the machine lists the chat itself', () => {
    expect(queuedRows([item({ state: 'delivered', convId: 'tab-x' })], () => null)).toEqual([]);
  });

  it('says what it waits for on its screen', () => {
    expect(queuedViewOf(item())).toBe(QUEUED_VIEW.waiting);
    expect(queuedViewOf(item({ state: 'sending' }))).toBe(QUEUED_VIEW.starting);
    expect(queuedViewOf(item({ state: 'delivered', convId: 'tab-x' }))).toBe(QUEUED_VIEW.starting);
    expect(queuedViewOf(item({ state: 'failed' }))).toBe(QUEUED_VIEW.failed);
    expect(queuedViewOf(null)).toBe(QUEUED_VIEW.gone);
  });
});
