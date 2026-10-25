import { describe, expect, it } from 'vitest';
import { excerptOf, summaryOf, SUMMARY_CHARS } from '@/next/features/subagents/excerpt';

describe('the excerpt of what a subagent returned', () => {
  it('is the first three lines without the empty ones, and says when there is more', () => {
    const out = excerptOf('one\n\ntwo\n\nthree\nfour');
    expect(out).toEqual({ text: 'one\ntwo\nthree', more: true });
  });

  it('is all of a short answer', () => {
    expect(excerptOf('done')).toEqual({ text: 'done', more: false });
  });

  it('closes a code block it cut open', () => {
    const out = excerptOf('Result:\n```ts\nconst a = 1;\nconst b = 2;');
    expect(out.text.endsWith('\n```')).toBe(true);
    expect(out.more).toBe(true);
  });
});

describe('the one-line summary of what a subagent returned', () => {
  it('is plain text: code spans and bold lose their marks, links keep their label', () => {
    const out = summaryOf('Сделано в **одном** коммите `03f9c3e`, см. [отчёт](https://x.y/z).');
    expect(out).toBe('Сделано в одном коммите 03f9c3e, см. отчёт.');
  });

  it('drops code blocks and the marks of headings and lists', () => {
    const out = summaryOf('## Итог\n\n- первое\n- второе\n\n```ts\nconst a = 1;\n```\nконец');
    expect(out).toBe('Итог первое второе конец');
  });

  it('ends a long answer at a sentence, or with an ellipsis when none fits', () => {
    const sentence = `${'слово '.repeat(40)}конец.`;
    expect(summaryOf(`${sentence} Ещё ${'слово '.repeat(40)}`).endsWith('конец.')).toBe(true);
    const run = summaryOf('слово '.repeat(100));
    expect(run.endsWith('…')).toBe(true);
    expect(run.length).toBeLessThanOrEqual(SUMMARY_CHARS + 1);
  });
});
