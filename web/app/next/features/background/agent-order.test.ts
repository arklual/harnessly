import { describe, expect, it } from 'vitest';
import { AGENTS_FIRST, orderAgents, splitAgents } from '@/next/features/background/agent-order';
import type { BackgroundTask } from '@/next/protocol/v2';

const task = (id: string, status: BackgroundTask['status'], startedAt: number): BackgroundTask =>
  ({ id, status, startedAt, kind: 'subagent', title: id }) as BackgroundTask;

describe('orderAgents', () => {
  it('puts the running first, then the newest start', () => {
    const list = [
      task('old-done', 'done', 1),
      task('new-done', 'done', 9),
      task('old-run', 'running', 2),
      task('new-run', 'running', 8),
    ];
    expect(orderAgents(list).map((x) => x.id)).toEqual(['new-run', 'old-run', 'new-done', 'old-done']);
  });

  it('puts an idle teammate after the running and before the ones over', () => {
    const list = [task('done', 'done', 9), task('idle', 'idle', 1), task('run', 'running', 2)];
    expect(orderAgents(list).map((x) => x.id)).toEqual(['run', 'idle', 'done']);
  });

  it('does not touch the list it was given', () => {
    const list = [task('a', 'done', 1), task('b', 'running', 2)];
    orderAgents(list);
    expect(list.map((x) => x.id)).toEqual(['a', 'b']);
  });
});

describe('splitAgents', () => {
  it('shows five rows and keeps the rest behind the fold', () => {
    const list = Array.from({ length: 9 }, (_, i) => task(`t${i}`, 'running', i));
    const { head, rest } = splitAgents(list);
    expect(head).toHaveLength(AGENTS_FIRST);
    expect(rest).toHaveLength(4);
    expect(head[0].id).toBe('t8');
  });

  it('has no rest for a short list', () => {
    expect(splitAgents([task('a', 'running', 1)]).rest).toEqual([]);
  });
});
