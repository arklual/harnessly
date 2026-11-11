import { describe, expect, it } from 'vitest';
import {
  applyReplacement,
  gitlabSuggestion,
  latestSuggestions,
  parseSuggestions,
  reviewRequest,
} from '@/next/features/review/app/agent-review';

const block = (json: string) => `Посмотрел.\n\n\`\`\`review-suggestions\n${json}\n\`\`\`\nВсё.`;

describe('parseSuggestions', () => {
  it('reads the last block, keeps the good entries and drops the bad ones', () => {
    const text =
      block('[{"path":"old.go","from":1,"text":"x"}]') +
      block(
        '[{"path":"a.go","from":3,"to":4,"text":" rename ","replacement":"b := 1\\n"},' +
          '{"path":"b.go","line":7,"side":"before","text":"why"},{"path":"","from":1,"text":"x"},' +
          '{"path":"c.go","from":5,"to":2,"text":"x"},{"path":"d.go","from":1,"text":""}]',
      );
    const got = parseSuggestions(text);
    expect(got?.map((s) => [s.path, s.side, s.from, s.to, s.text, s.replacement])).toEqual([
      ['a.go', 'after', 3, 4, 'rename', 'b := 1\n'],
      ['b.go', 'before', 7, 7, 'why', undefined],
    ]);
    expect(got?.[0]?.id).toBe(parseSuggestions(text)?.[0]?.id);
  });
  it('is null without a block and empty for a block that is not JSON', () => {
    expect(parseSuggestions('no block')).toBeNull();
    expect(parseSuggestions(block('not json'))).toEqual([]);
  });
  it('takes the newest answer that has a block', () => {
    expect(latestSuggestions([block('[{"path":"a","from":1,"text":"x"}]'), 'plain', 'more'])?.[0]?.path).toBe(
      'a',
    );
    expect(latestSuggestions(['plain'])).toBeNull();
  });
});

it('replaces lines with the replacement', () => {
  expect(applyReplacement('a\nb\nc\nd', 2, 3, 'x\ny\nz\n')).toBe('a\nx\ny\nz\nd');
  expect(applyReplacement('a\nb', 1, 1, '')).toBe('\nb');
});

it('makes a GitLab suggestion over the lines above the note', () => {
  expect(gitlabSuggestion({ from: 3, to: 4, text: 'rename', replacement: 'b := 1\n' })).toBe(
    'rename\n\n```suggestion:-1+0\nb := 1\n```',
  );
  expect(gitlabSuggestion({ from: 3, to: 3, text: 'only words' })).toBe('only words');
});

it('asks for the block the page reads', () => {
  const text = reviewRequest([{ path: 'a.go', additions: 2, deletions: 1 }], 'https://x/-/merge_requests/1', {
    ask: 'Пройди ревью',
    format: 'Ответь блоком:',
  });
  expect(text).toContain('- a.go (+2 −1)');
  expect(parseSuggestions(text)?.[0]?.path).toBe('src/a.ts');
});
