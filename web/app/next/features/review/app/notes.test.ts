import { describe, expect, it } from 'vitest';
import {
  agentMessage,
  draftsOf,
  linesOf,
  newNoteId,
  placeNote,
  questionMessage,
  type IReviewNote,
} from '@/next/features/review/app/notes';

const words = { intro: 'Замечания:', more: 'Ещё:', before: 'старая версия', question: 'Вопрос:' };

const note = (over: Partial<IReviewNote>): IReviewNote => ({
  id: 'n',
  path: 'a.go',
  side: 'after',
  from: 2,
  to: 3,
  lines: 'b\nc',
  text: 'rename',
  state: 'draft',
  createdAt: 1,
  updatedAt: 1,
  ...over,
});

describe('placeNote', () => {
  const doc = ['a', 'b', 'c', 'd'];
  it('keeps the lines where they are', () => {
    expect(placeNote(note({}), doc)).toEqual({ from: 2, to: 3 });
  });
  it('follows the lines when lines were added above', () => {
    expect(placeNote(note({}), ['x', 'y', ...doc])).toEqual({ from: 4, to: 5 });
  });
  it('picks the nearest of several copies', () => {
    expect(placeNote(note({ from: 6, to: 7 }), ['b', 'c', 'x', 'x', 'b', 'c', 'x'])).toEqual({
      from: 5,
      to: 6,
    });
  });
  it('is outdated when the lines are gone', () => {
    expect(placeNote(note({}), ['a', 'B', 'c'])).toBeNull();
    expect(placeNote(note({ from: 9, to: 10 }), [])).toBeNull();
  });
});

describe('messages', () => {
  it('lists the drafts by file and line, with the fragment and what the person adds', () => {
    const drafts = draftsOf([
      note({ id: '2', path: 'b.go', from: 1, to: 1, lines: 'x', text: 'two' }),
      note({ id: 's', state: 'sent' }),
      note({ id: '1', text: ' one ' }),
    ]);
    expect(drafts.map((n) => n.id)).toEqual(['1', '2']);
    expect(agentMessage(drafts, ' and tests ', words)).toBe(
      'Замечания:\n\n1. a.go:2-3\n```\nb\nc\n```\none\n\n2. b.go:1\n```\nx\n```\ntwo\n\nЕщё:\nand tests',
    );
  });
  it('fences a fragment that has a fence of its own, and says when the lines are of the old version', () => {
    const at = note({ side: 'before', from: 4, to: 4, lines: 'a ``` b' });
    expect(questionMessage(at, 'why?', words)).toBe(
      'Вопрос:\n\na.go:4 (старая версия)\n````\na ``` b\n````\n\nwhy?',
    );
  });
});

it('reads lines and makes ids the hub keeps', () => {
  expect(linesOf(['a', 'b', 'c'], 2, 3)).toBe('b\nc');
  expect(newNoteId()).toMatch(/^[a-z0-9]{16}$/);
});
