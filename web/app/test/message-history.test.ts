import { describe, expect, it } from 'vitest';
import {
  HISTORY_LIMIT,
  NOT_RECALLING,
  historyText,
  stepRecall,
  withEntry,
  withoutEntry,
  type IHistoryEntry,
} from '@/lib/hl-message-history';
import { applyOrder, enabledQuickPrompts } from '@/lib/hl-quick-prompts';

const entry = (id: string, message: string): IHistoryEntry => ({
  id,
  message,
  sentAt: '2026-10-22T10:00:00Z',
});

describe('historyText', () => {
  it('keeps what was typed and drops the blocks the composer appended', () => {
    const sent =
      'Fix the header\n\n<annotation kind="element">\nbutton.primary\n</annotation>\n\n<diff-comment file="a.ts">\nrename\n</diff-comment>';
    expect(historyText(sent)).toBe('Fix the header');
  });

  it('does not keep slash commands or empty text', () => {
    expect(historyText('/compact')).toBe('');
    expect(historyText('  \n ')).toBe('');
  });
});

describe('withEntry', () => {
  it('puts the entry first and drops an older one with the same text', () => {
    const list = [entry('1', 'b'), entry('2', 'a')];
    expect(withEntry(list, entry('3', 'a')).map((e) => e.id)).toEqual(['3', '1']);
  });

  it('keeps the limit', () => {
    const list = Array.from({ length: HISTORY_LIMIT }, (_, i) => entry(String(i), `m${i}`));
    const next = withEntry(list, entry('new', 'fresh'));
    expect(next).toHaveLength(HISTORY_LIMIT);
    expect(next[0].id).toBe('new');
  });

  it('removes by id', () => {
    expect(withoutEntry([entry('1', 'a'), entry('2', 'b')], '1').map((e) => e.id)).toEqual(['2']);
  });
});

describe('stepRecall', () => {
  const list = [entry('1', 'newest'), entry('2', 'middle'), entry('3', 'oldest')];

  it('leaves the arrows alone while the person writes', () => {
    expect(stepRecall(list, NOT_RECALLING, 'up', 'typing').value).toBeNull();
    expect(stepRecall(list, NOT_RECALLING, 'down', '').value).toBeNull();
    expect(stepRecall([], NOT_RECALLING, 'up', '').value).toBeNull();
  });

  it('walks back from an empty field and returns to the draft', () => {
    const first = stepRecall(list, NOT_RECALLING, 'up', '');
    expect(first.value).toBe('newest');
    const second = stepRecall(list, first.recall, 'up', 'newest');
    expect(second.value).toBe('middle');
    const third = stepRecall(list, second.recall, 'up', 'middle');
    expect(third.value).toBe('oldest');
    expect(stepRecall(list, third.recall, 'up', 'oldest').value).toBeNull();
    const back = stepRecall(list, third.recall, 'down', 'oldest');
    expect(back.value).toBe('middle');
    const newest = stepRecall(list, back.recall, 'down', 'middle');
    expect(newest.value).toBe('newest');
    const empty = stepRecall(list, newest.recall, 'down', 'newest');
    expect(empty.value).toBe('');
    expect(empty.recall).toEqual(NOT_RECALLING);
  });

  it('stops walking once the recalled text was edited', () => {
    const first = stepRecall(list, NOT_RECALLING, 'up', '');
    expect(stepRecall(list, first.recall, 'up', 'newest edited').value).toBeNull();
  });
});

describe('quick prompts', () => {
  const p = (id: string, enabled = true, prompt = `text ${id}`) => ({ id, name: id, prompt, enabled });

  it('orders by the saved order and appends the rest', () => {
    expect(applyOrder([{ id: 'a' }, { id: 'b' }, { id: 'c' }], ['c', 'x', 'a']).map((i) => i.id)).toEqual([
      'c',
      'a',
      'b',
    ]);
  });

  it('shows enabled built-in and custom prompts only', () => {
    const data = {
      builtins: [p('b1'), p('b2', false)],
      custom: [p('c1'), p('c2', true, '  ')],
      order: ['c1', 'b1'],
    };
    expect(enabledQuickPrompts(data).map((x) => x.id)).toEqual(['c1', 'b1']);
  });

  it('survives a missing or foreign-shaped answer', () => {
    expect(enabledQuickPrompts(undefined)).toEqual([]);
    expect(enabledQuickPrompts({ prompts: [] } as never)).toEqual([]);
  });
});
