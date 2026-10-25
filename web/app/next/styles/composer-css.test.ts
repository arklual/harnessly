import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// The tests run without CSS (vite.config.ts: css: false), so the sheet is read as text.
const css = readFileSync(join(__dirname, 'composer.css'), 'utf8');

/** The blocks of the at-rules whose prelude starts with `at`, with what they hold. */
const blocksOf = (source: string, at: string): string[] => {
  const out: string[] = [];
  let from = source.indexOf(at);
  while (from >= 0) {
    const open = source.indexOf('{', from);
    let depth = 0;
    let end = open;
    for (; end < source.length; end++) {
      if (source[end] === '{') depth++;
      if (source[end] === '}' && --depth === 0) break;
    }
    out.push(source.slice(from, end + 1));
    from = source.indexOf(at, end);
  }
  return out;
};

// jsdom lays nothing out, so the field's look by its own width is checked in its rules: one place, a container query.
describe('the narrow field', () => {
  const field = blocksOf(css, '@container hx-field');
  const media = blocksOf(css, '@media (max-width:');

  it('is a query container of its own width, the field of a chat and the start of a design alike', () => {
    expect(css).toMatch(/\.hx-composer,\s*\.hx-dl__composer\s*\{\s*container: hx-field \/ inline-size;/);
  });

  it("has the phone's look under one container query: the pill with its name, the ring and idle limits gone", () => {
    expect(field).toHaveLength(1);
    const [rules] = field;
    expect(rules).toMatch(/^@container hx-field \(max-width: 580px\)/);
    for (const part of [
      '--hx-composer-h: 96px',
      '.hx-model__effort,\n  .hx-model__chev {\n    display: none;',
      ".hx-composer__bar .hx-lim:not(:has([data-level='low'], [data-level='out']))",
      '.hx-composer__bar .hx-ctx',
      '.hx-dock .hx-composer__field',
    ])
      expect(rules).toContain(part);
  });

  it('is not written again for the window: no viewport rule repeats the field or the pill', () => {
    for (const rules of media) {
      expect(rules).not.toMatch(
        /\.hx-model\b|\.hx-composer__(layer|bar|field|input)\b|\.hx-lim\b|\.hx-ctx\b/,
      );
    }
  });
});
