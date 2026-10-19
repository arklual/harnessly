import { describe, expect, it } from 'vitest';

import {
  callStatus,
  emptyTimeline,
  groupTimeline,
  prependEntries,
  reduceTimeline,
  type TimelineEntry,
  workSummary,
} from './timeline';

const e = (id: string, type: string, extra: Record<string, unknown> = {}): TimelineEntry => ({
  id,
  type,
  timestamp: 1,
  ...extra,
});

const init = {
  type: 'timeline:init',
  entries: [
    e('entry-1', 'user-message', { text: 'Fix the parser' }),
    e('entry-2', 'assistant-message', { markdown: 'Looking.' }),
    e('entry-3', 'tool-call', { toolUseId: 'toolu_1', toolName: 'Bash', summary: '$ ls', status: 'pending' }),
    e('entry-4', 'tool-result', { toolUseId: 'toolu_1', isError: false, summary: '2 lines' }),
  ],
  sessionId: 's-1',
  hasMore: true,
  startByteOffset: 900,
  jsonlPath: '/home/agent/.claude/projects/x/s-1.jsonl',
};

describe('reduceTimeline', () => {
  it('replaces the view on init and folds results into their calls', () => {
    const s = reduceTimeline({ ...emptyTimeline, liveText: 'x' }, init);
    expect(s.entries.map((x) => x.id)).toEqual(['entry-1', 'entry-2', 'entry-3']);
    expect(s.results.toolu_1).toEqual({ isError: false, summary: '2 lines' });
    expect(callStatus(s.entries[2]!, s.results)).toBe('success');
    expect(s).toMatchObject({
      sessionId: 's-1',
      hasMore: true,
      startByteOffset: 900,
      liveText: '',
      loaded: true,
    });
  });

  it('marks an empty init as waiting for the agent', () => {
    const s = reduceTimeline(emptyTimeline, { type: 'timeline:init', entries: [], sessionId: '' });
    expect(s.waiting).toBe(true);
  });

  it('appends new entries once, and an error result fails its call', () => {
    let s = reduceTimeline(emptyTimeline, init);
    s = reduceTimeline(s, { type: 'timeline:live-text', text: 'typing' });
    s = reduceTimeline(s, {
      type: 'timeline:append',
      entries: [
        e('entry-3', 'tool-call', { toolUseId: 'toolu_1' }),
        e('entry-5', 'tool-call', { toolUseId: 'toolu_2', toolName: 'Edit' }),
        e('entry-6', 'tool-result', { toolUseId: 'toolu_2', isError: true, summary: 'no' }),
      ],
    });
    expect(s.entries.map((x) => x.id)).toEqual(['entry-1', 'entry-2', 'entry-3', 'entry-5']);
    expect(callStatus(s.entries[3]!, s.results)).toBe('error');
    expect(s.liveText).toBe('');
  });

  it('puts older pages in front, or replaces them when told', () => {
    const s = reduceTimeline(emptyTimeline, init);
    const p = prependEntries(s, {
      entries: [e('entry-0', 'user-message')],
      startByteOffset: 0,
      hasMore: false,
    });
    expect(p.entries[0]?.id).toBe('entry-0');
    expect(p.hasMore).toBe(false);
    const r = prependEntries(s, {
      entries: [e('a', 'user-message')],
      startByteOffset: 0,
      hasMore: false,
      replaceEntries: true,
    });
    expect(r.entries.map((x) => x.id)).toEqual(['a']);
  });

  it('keeps the error text', () => {
    const s = reduceTimeline(emptyTimeline, {
      type: 'timeline:error',
      code: 'parse-error',
      message: 'bad line',
    });
    expect(s.error).toBe('bad line');
  });
});

describe('groupTimeline', () => {
  it('folds the work between messages', () => {
    const items = groupTimeline([
      e('1', 'user-message'),
      e('2', 'thinking'),
      e('3', 'tool-call', { toolName: 'Read', summary: 'a.go' }),
      e('4', 'tool-call', { toolName: 'Edit', summary: 'a.go' }),
      e('5', 'assistant-message'),
      e('6', 'turn-end'),
      e('7', 'interrupt'),
    ]);
    expect(items.map((i) => i.kind)).toEqual(['user', 'work', 'assistant', 'notice']);
    const work = items[1];
    expect(work?.kind === 'work' && workSummary(work.entries)).toBe('2 steps · 1 thought — Edit a.go');
  });
});
