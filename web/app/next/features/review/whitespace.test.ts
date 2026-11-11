import { describe, expect, it } from 'vitest';
import { ignoreWhitespaceDiff, normalizeLine } from '@/next/features/review/whitespace';

describe('a diff that ignores whitespace', () => {
  it('sees no change in re-indented lines', () => {
    expect(ignoreWhitespaceDiff('if (a) {\n  b();\n}', 'if (a) {\n    b();\n}')).toEqual([]);
    expect(normalizeLine('  a \t b  ')).toBe('a b');
  });

  it('still finds a changed line, in characters of the original texts', () => {
    const a = 'one\n  two\nthree';
    const b = 'one\n    TWO\nthree';
    const changes = ignoreWhitespaceDiff(a, b);
    expect(changes.length).toBeGreaterThan(0);
    for (const change of changes) {
      expect(change.fromA).toBeGreaterThanOrEqual(4);
      expect(change.toA).toBeLessThanOrEqual(10);
      expect(change.fromB).toBeGreaterThanOrEqual(4);
      expect(change.toB).toBeLessThanOrEqual(12);
    }
    const inB = changes.map((c) => b.slice(c.fromB, c.toB)).join('');
    expect(inB).toContain('TWO');
  });

  it('finds added lines', () => {
    const changes = ignoreWhitespaceDiff('a\nc', 'a\nb\nc');
    expect(changes).toHaveLength(1);
    expect('a\nb\nc'.slice(changes[0]!.fromB, changes[0]!.toB)).toBe('b\n');
    expect(changes[0]!.fromA).toBe(changes[0]!.toA);
  });
});
