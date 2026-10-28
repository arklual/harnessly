import { describe, expect, it } from 'vitest';
import { DICT, DICT_IDLE, dictReduce, type IDictState } from '@/next/features/designly/deck/dictation-state';

const run = (events: Parameters<typeof dictReduce>[1][]): IDictState => events.reduce(dictReduce, DICT_IDLE);
const slide = { kind: 'slide', page: 'p1' } as const;

describe('dictation', () => {
  it('listens, collects words, tidies, ends', () => {
    const s = run([
      { k: 'start', target: slide },
      { k: 'words', raw: 'ну короче' },
    ]);
    expect(s).toEqual({ phase: DICT.listening, target: slide, raw: 'ну короче' });
    const t = dictReduce(s, { k: 'final', raw: 'ну короче MCP' });
    expect(t.phase).toBe(DICT.tidying);
    expect(t.raw).toBe('ну короче MCP');
    expect(dictReduce(t, { k: 'tidied' })).toEqual(DICT_IDLE);
  });
  it('a silence ends with nothing to tidy', () => {
    expect(run([{ k: 'start', target: slide }, { k: 'end' }])).toEqual(DICT_IDLE);
  });
  it('a second start while busy changes nothing', () => {
    const s = run([{ k: 'start', target: slide }]);
    expect(dictReduce(s, { k: 'start', target: { kind: 'all' } })).toBe(s);
  });
  it('a failed tidy lets go', () => {
    expect(run([{ k: 'start', target: slide }, { k: 'final', raw: 'x' }, { k: 'failed' }])).toEqual(
      DICT_IDLE,
    );
  });
});
