import { describe, expect, it } from 'vitest';
import {
  heardText,
  parseHeard,
  withoutWord,
  withWord,
  wordProblem,
  WORD_PROBLEM,
} from '@/next/features/settings/dictionary';
import { VOICE_LIMITS, type VoiceWord } from '@/next/stores/me';

const sinopia: VoiceWord = { word: 'Sinopia', heard: ['синопия'] };

describe('the dictation dictionary', () => {
  it('reads the variants apart by commas and lines, without blanks and repeats', () => {
    expect(parseHeard(' синопия, Синопия ;сенопия\n\n  хар  несли ')).toEqual([
      'синопия',
      'сенопия',
      'хар несли',
    ]);
    expect(parseHeard('')).toEqual([]);
    expect(heardText(['а', 'б'])).toBe('а, б');
  });

  it('takes a good entry and says what is wrong with a bad one', () => {
    expect(wordProblem([], sinopia, null)).toBeNull();
    expect(wordProblem([], { word: '', heard: [] }, null)).toBe(WORD_PROBLEM.empty);
    expect(wordProblem([], { word: 'a'.repeat(VOICE_LIMITS.wordLen + 1), heard: [] }, null)).toBe(
      WORD_PROBLEM.long,
    );
    expect(wordProblem([sinopia], { word: 'sinopia', heard: [] }, null)).toBe(WORD_PROBLEM.twice);
    expect(
      wordProblem(
        [],
        { word: 'x', heard: Array.from({ length: VOICE_LIMITS.heard + 1 }, (_, i) => `в${i}`) },
        null,
      ),
    ).toBe(WORD_PROBLEM.heardMany);
    const full = Array.from({ length: VOICE_LIMITS.words }, (_, i) => ({ word: `w${i}`, heard: [] }));
    expect(wordProblem(full, { word: 'new', heard: [] }, null)).toBe(WORD_PROBLEM.full);
  });

  it('lets the word being changed keep its own name', () => {
    expect(wordProblem([sinopia], { word: 'Sinopia', heard: ['сенопия'] }, 'Sinopia')).toBeNull();
    expect(
      wordProblem([sinopia, { word: 'harnessly', heard: [] }], { word: 'harnessly', heard: [] }, 'Sinopia'),
    ).toBe(WORD_PROBLEM.twice);
  });

  it('puts an entry in its place and takes one out', () => {
    const list = [sinopia, { word: 'harnessly', heard: [] }];
    expect(withWord(list, { word: 'Tiksly', heard: [] }, null).map((w) => w.word)).toEqual([
      'Sinopia',
      'harnessly',
      'Tiksly',
    ]);
    expect(withWord(list, { word: 'Sinopia', heard: ['сенопия'] }, 'sinopia')[0].heard).toEqual(['сенопия']);
    expect(withoutWord(list, 'SINOPIA').map((w) => w.word)).toEqual(['harnessly']);
  });
});
