import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

// A state a keystroke touches, inside :has(): the browser then checks every ancestor at each key pressed anywhere on the
// page and recomputes what those rules reach. One such rule on the sidebar's rows doubled the cost of a letter in every
// field. Put the state on the element from code, or use a sibling selector.
const COSTLY = /:has\([^{}]*:(focus-visible|placeholder-shown)/;

describe('the stylesheets', () => {
  it('keep what a keystroke changes out of :has()', () => {
    const dir = join(process.cwd(), 'app/next/styles');
    const found = readdirSync(dir)
      .filter((name) => name.endsWith('.css'))
      .flatMap((name) =>
        readFileSync(join(dir, name), 'utf8')
          .split('\n')
          .filter((line) => COSTLY.test(line))
          .map((line) => `${name}: ${line.trim()}`),
      );
    expect(found).toEqual([]);
  });
});
