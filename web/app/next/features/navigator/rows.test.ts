import { describe, expect, it } from 'vitest';
import { filterGroups, navigatorGroups, type INavSource } from '@/next/features/navigator/rows';
import type { Entry } from '@/next/protocol/v2';

const user = (id: string, text: string) =>
  ({ id, t: 'user', text, at: 1, turnId: id, rev: 0, attachments: [], clientId: null }) as unknown as Entry;
const agent = (id: string, description: string) =>
  ({ id, t: 'subagent', description, agentType: 'explore', at: 2, turnId: 't1', rev: 0 }) as unknown as Entry;

const source: INavSource = {
  turns: [
    { id: 't1', entryIds: ['t1', 's1'] },
    { id: 't2', entryIds: ['t2'] },
  ],
  entry: (id) =>
    ({ t1: user('t1', 'first\n prompt'), t2: user('t2', 'second'), s1: agent('s1', 'look around') })[id],
};
const labels = { prompts: 'P', agents: 'A' };

describe('navigatorGroups', () => {
  it('lists prompts newest first and subagents, one line each', () => {
    const groups = navigatorGroups(source, labels, () => 'now');
    expect(groups.map((g) => g.id)).toEqual(['prompts', 'agents']);
    expect(groups[0].rows.map((r) => r.title)).toEqual(['second', 'first prompt']);
    expect(groups[1].rows[0].target?.turnId).toBe('t1');
  });
  it('leaves an empty chat without groups', () => {
    expect(navigatorGroups({ turns: [], entry: () => undefined }, labels, () => '')).toEqual([]);
  });
});

describe('filterGroups', () => {
  it('keeps rows with every word and drops empty groups', () => {
    const groups = navigatorGroups(source, labels, () => 'now');
    expect(filterGroups(groups, 'LOOK').map((g) => g.id)).toEqual(['agents']);
    expect(filterGroups(groups, 'first prompt')[0].rows).toHaveLength(1);
    expect(filterGroups(groups, '')).toHaveLength(2);
  });
});
