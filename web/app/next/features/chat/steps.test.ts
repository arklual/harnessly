import { describe, expect, it } from 'vitest';
import { dedupeTools, groupSteps } from '@/next/features/chat/steps';
import type { Entry, SubagentEntry, ToolEntry } from '@/next/features/chat/types';

const agent = (id: string): SubagentEntry => ({
  id,
  turnId: null,
  at: 1,
  rev: 1,
  t: 'subagent',
  agentId: id,
  agentType: 'ci',
  description: 'Speed up CI',
  status: 'running',
  model: null,
  durationMs: null,
  output: null,
});
const say = (id: string): Entry => ({ id, turnId: null, at: 1, rev: 1, t: 'say', md: 'text' });

describe('grouping the steps of a turn', () => {
  it('puts the subagents that follow each other into one block', () => {
    const nodes = groupSteps([agent('a'), agent('b'), say('s'), agent('c')], false);
    expect(nodes.map((node) => node.kind)).toEqual(['subs', 'entry', 'subs']);
    expect(nodes[0]).toMatchObject({ agents: [{ id: 'a' }, { id: 'b' }] });
  });

  it('shows one step for a tool call logged twice, settled by the result of either', () => {
    const call = (id: string, ok: boolean | null): ToolEntry => ({
      id,
      turnId: null,
      at: 1,
      rev: 1,
      t: 'tool',
      toolUseId: 'toolu_1',
      tool: 'Bash',
      summary: '$ cd /work',
      ok,
      output: null,
    });
    const nodes = groupSteps([call('a', null), say('s'), call('b', true)], false);
    expect(nodes.map((node) => node.kind)).toEqual(['entry', 'entry']);
    expect(nodes[0]).toMatchObject({ entry: { id: 'a', ok: true } });
    expect(dedupeTools([call('a', null), call('b', null)])).toHaveLength(1);
  });
});
