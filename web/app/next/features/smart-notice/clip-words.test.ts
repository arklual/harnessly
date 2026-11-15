import { describe, expect, it } from 'vitest';
import { clipWords, SNIPPET_MAX } from './clip-words';

describe('clipWords', () => {
  it('keeps a short text whole', () => {
    expect(clipWords('Готово, тесты зелёные.')).toBe('Готово, тесты зелёные.');
  });

  it('cuts a long text at a word, never inside one', () => {
    const text = 'Слово '.repeat(40).trim();
    const out = clipWords(text, 30);
    expect(out.endsWith('Слово…')).toBe(true);
    expect([...out].length).toBeLessThanOrEqual(31);
  });

  it('drops the broken last word of a text the machine cut at its limit', () => {
    const text = `${'а'.repeat(SNIPPET_MAX - 12)} Отдал подча`;
    expect([...text].length).toBe(SNIPPET_MAX);
    const out = clipWords(text, 400);
    expect(out.endsWith('Отдал…')).toBe(true);
  });

  it('drops the punctuation before the «…»', () => {
    expect(clipWords('Первое, второе, третье слово', 16)).toBe('Первое, второе…');
  });
});
