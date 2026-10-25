import { describe, expect, it } from 'vitest';
import { tailThought } from '@/next/features/chat/now-thought';
import type { Entry } from '@/next/features/chat/types';

const think = (id: string, text: string): Entry =>
  ({ id, turnId: null, at: 0, rev: 1, t: 'think', text }) as Entry;
const tool = (id: string): Entry =>
  ({
    id,
    turnId: null,
    at: 0,
    rev: 1,
    t: 'tool',
    toolUseId: id,
    tool: 'Bash',
    summary: 'ls',
    ok: null,
  }) as Entry;

describe('tailThought', () => {
  const said =
    'Очистил кэши Go и npm, освободив 4.9 ГБ — ищу, что ещё можно удалить. Затем вернусь к интерфейсу.\n\n';

  it('drops the thought the steps already show, as the machine clipped and collapsed it', () => {
    const now = said.replace(/\s+/g, ' ').slice(0, 60);
    expect(tailThought(now, [think('a', said), tool('b')])).toBeNull();
  });

  it('keeps a thought the steps do not show', () => {
    expect(tailThought('Проверяю свёртку', [think('a', said), tool('b')])).toBe('Проверяю свёртку');
  });

  it('keeps the thought while the steps are not loaded, and has none without one', () => {
    expect(tailThought('Думаю', null)).toBe('Думаю');
    expect(tailThought(null, [])).toBeNull();
  });
});
