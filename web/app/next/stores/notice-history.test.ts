import { describe, expect, it } from 'vitest';
import { MAX_HISTORY, parseHistory } from '@/next/stores/notice-history';

const entry = (key: string) => ({ key, chatId: null, icon: 'check', title: 't', text: 'x', at: 1 });

describe('parseHistory', () => {
  it('reads what is well formed and drops the rest', () => {
    const raw = JSON.stringify([entry('a'), { key: 'b' }, entry('c'), null]);
    expect(parseHistory(raw).map((e) => e.key)).toEqual(['a', 'c']);
  });

  it('keeps a short list and survives what is not a list', () => {
    const many = Array.from({ length: MAX_HISTORY + 5 }, (_, i) => entry(`k${i}`));
    expect(parseHistory(JSON.stringify(many))).toHaveLength(MAX_HISTORY);
    expect(parseHistory('{')).toEqual([]);
    expect(parseHistory(null)).toEqual([]);
    expect(parseHistory('{"a":1}')).toEqual([]);
  });
});
