import { describe, expect, it } from 'vitest';
import {
  NOTICE_PREVIEW,
  answeredLabels,
  compactionSaving,
  formatDuration,
  formatTokens,
  noticePreview,
  patchFileAction,
  planTitle,
} from '@/lib/timeline-items';
import type { IAskUserQuestionItem } from '@/types/timeline';

describe('planTitle', () => {
  it('takes the first line with text and drops the heading marks', () => {
    expect(planTitle('\n\n## Move the feed\n\n- step')).toBe('Move the feed');
  });
  it('is empty for an empty plan', () => {
    expect(planTitle('  \n\n')).toBe('');
  });
});

describe('answeredLabels', () => {
  const question: IAskUserQuestionItem = {
    question: 'Which one?',
    header: 'Pick',
    multiSelect: true,
    preview: false,
    options: [
      { label: 'A', description: '' },
      { label: 'B', description: '' },
      { label: 'C', description: '' },
    ],
  };
  it('is null while unanswered', () => {
    expect(answeredLabels(question, undefined)).toBeNull();
    expect(answeredLabels(question, { Other: 'A' })).toBeNull();
  });
  it('finds the picked options and keeps the free text', () => {
    expect(answeredLabels(question, { 'Which one?': 'A, C' })).toEqual({ picked: [0, 2], other: '' });
    expect(answeredLabels(question, { 'Which one?': 'my own' })?.other).toBe('my own');
  });
});

describe('formatDuration', () => {
  it('uses the shortest unit', () => {
    expect(formatDuration(420)).toBe('420 ms');
    expect(formatDuration(3200)).toBe('3.2 s');
    expect(formatDuration(65_000)).toBe('1 min 05 s');
  });
  it('is empty for nonsense', () => {
    expect(formatDuration(-1)).toBe('');
    expect(formatDuration(Number.NaN)).toBe('');
  });
});

describe('formatTokens', () => {
  it('shortens thousands', () => {
    expect(formatTokens(842)).toBe('842');
    expect(formatTokens(12_400)).toBe('12.4k');
  });
});

describe('compactionSaving', () => {
  it('gives the freed share in percent', () => {
    expect(compactionSaving(100_000, 25_000)).toBe(75);
  });
  it('is null when a side is unknown or nothing was freed', () => {
    expect(compactionSaving(undefined, 10)).toBeNull();
    expect(compactionSaving(100, undefined)).toBeNull();
    expect(compactionSaving(100, 100)).toBeNull();
    expect(compactionSaving(0, 0)).toBeNull();
  });
});

describe('noticePreview', () => {
  it('shows a short message whole', () => {
    expect(noticePreview('boom', false)).toEqual({ long: false, text: 'boom' });
  });
  it('folds a long message or one with line breaks until opened', () => {
    const long = 'x'.repeat(NOTICE_PREVIEW + 50);
    expect(noticePreview(long, false).text).toBe(`${'x'.repeat(NOTICE_PREVIEW)}…`);
    expect(noticePreview(long, true).text).toBe(long);
    expect(noticePreview('a\nb', false).long).toBe(true);
  });
});

describe('patchFileAction', () => {
  it('reduces the words of the runtime to three actions', () => {
    expect(patchFileAction('Add')).toBe('create');
    expect(patchFileAction('create')).toBe('create');
    expect(patchFileAction('delete')).toBe('delete');
    expect(patchFileAction('Update')).toBe('modify');
    expect(patchFileAction('rename')).toBe('other');
    expect(patchFileAction(undefined)).toBe('other');
  });
});
