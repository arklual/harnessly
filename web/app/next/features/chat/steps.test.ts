import { describe, expect, it } from 'vitest';
import { groupSteps } from '@/next/features/chat/steps';
import type { Entry, SubagentEntry } from '@/next/features/chat/types';

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
});
