import { describe, expect, it } from 'vitest';
import { groupTimelineItems, isSubagentCall, toolStatusOf } from '@/lib/timeline-grouping';
import type { ITimelineEntry, ITimelineToolCall } from '@/types/timeline';

const call = (id: string, toolName = 'Read', extra: Partial<ITimelineToolCall> = {}): ITimelineToolCall => ({
  id,
  type: 'tool-call',
  timestamp: 1,
  toolUseId: `u-${id}`,
  toolName,
  summary: id,
  status: 'pending',
  ...extra,
});

describe('groupTimelineItems', () => {
  it('folds consecutive tool calls with their results into one group', () => {
    const entries: ITimelineEntry[] = [
      { id: 'm', type: 'user-message', timestamp: 0, text: 'go' },
      call('a'),
      { id: 'r', type: 'tool-result', timestamp: 2, toolUseId: 'u-a', isError: false, summary: '' },
      call('b', 'Bash'),
      { id: 'reply', type: 'assistant-message', timestamp: 3, markdown: 'done' },
    ];
    const items = groupTimelineItems(entries);
    expect(items.map((i) => i.type)).toEqual(['entry', 'tool-group', 'entry']);
    const group = items[1];
    if (group?.type !== 'tool-group') throw new Error('no group');
    expect(group.toolCalls.map((c) => c.id)).toEqual(['a', 'b']);
    expect(group.toolResults.map((r) => r.id)).toEqual(['r']);
    expect(toolStatusOf(group.toolCalls[0]!, group.toolResults)).toBe('success');
    expect(toolStatusOf(group.toolCalls[1]!, group.toolResults)).toBe('pending');
  });

  it('keeps subagent calls and calls that carry files or messages as entries of their own', () => {
    const items = groupTimelineItems([
      call('a'),
      call('task', 'Task'),
      call('send', 'SendUserFile', { sentFiles: { paths: ['/x.png'] } }),
      call('b'),
    ]);
    expect(items.map((i) => (i.type === 'entry' ? i.entry.id : `group:${i.toolCalls.length}`))).toEqual([
      'group:1',
      'task',
      'send',
      'group:1',
    ]);
    expect(isSubagentCall(call('x', 'Agent'))).toBe(true);
  });

  it('starts a new group after the end of a turn', () => {
    const items = groupTimelineItems([call('a'), { id: 'e', type: 'turn-end', timestamp: 2 }, call('b')]);
    expect(items.filter((i) => i.type === 'tool-group')).toHaveLength(2);
  });
});
