import { describe, expect, it } from 'vitest';
import { progressPhrase, shouldSay } from '@/next/features/voice-live/progress';
import type { Now } from '@/next/protocol/v2';

const now = (tool: string, summary: string): Now =>
  ({
    turnId: 't',
    thought: null,
    action: { tool, summary, command: null },
    word: null,
    at: 1,
  }) as unknown as Now;

describe('progressPhrase', () => {
  it('says the step in plain words, without tool names', () => {
    expect(progressPhrase(now('Read', 'project/Оплата.dc.html'))).toBe('Смотрю доску «Оплата»');
    expect(progressPhrase(now('Edit', '/srv/x/project/Главная.dc.html'))).toBe('Правлю доску «Главная»');
    expect(progressPhrase(now('Bash', 'pnpm test'))).toBe('Проверяю на машине');
    expect(progressPhrase(now('mcp__preview__chrome_screenshot', ''))).toBe('Смотрю, как получилось');
    expect(progressPhrase(now('mcp__whatever__unknown_thing', 'x'))).toBeNull();
    expect(progressPhrase(null)).toBeNull();
  });
});

describe('shouldSay', () => {
  it('says a new phrase at most every 3.5 s', () => {
    expect(shouldSay('Смотрю', null, 0)).toBe(true);
    expect(shouldSay('Смотрю', { text: 'Смотрю', at: 0 }, 10_000)).toBe(false);
    expect(shouldSay('Правлю', { text: 'Смотрю', at: 0 }, 1000)).toBe(false);
    expect(shouldSay('Правлю', { text: 'Смотрю', at: 0 }, 4000)).toBe(true);
  });
});
