import { describe, expect, it } from 'vitest';
import {
  nextPart,
  pauseReading,
  reading,
  readingParts,
  resumeReading,
  startReading,
} from '@/next/features/voice-live/reader';

describe('a long reading', () => {
  it('cuts at sentence ends, never over the size unless one sentence is', () => {
    const text = Array.from(
      { length: 40 },
      (_, i) => `Предложение номер ${i} довольно длинное, чтобы было что резать.`,
    ).join(' ');
    const parts = readingParts(text, 300);
    expect(parts.length).toBeGreaterThan(5);
    for (const p of parts) {
      expect(p.length).toBeLessThanOrEqual(300);
      expect(p.endsWith('.')).toBe(true);
    }
    expect(parts.join(' ')).toBe(text);
  });
  it('goes part by part, pauses and goes on, and ends', () => {
    startReading('t', 'Раз. Два. Три.'.repeat(1));
    expect(reading()?.parts.length).toBe(1);
    startReading(
      't',
      Array.from({ length: 30 }, () => 'Слово слово слово слово слово слово слово слово.').join(' '),
    );
    const n = reading()!.parts.length;
    pauseReading();
    expect(reading()?.paused).toBe(true);
    resumeReading();
    for (let i = 1; i < n; i++) expect(nextPart()).not.toBeNull();
    expect(nextPart()).toBeNull();
    expect(reading()).toBeNull();
  });
});
