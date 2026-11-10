import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

// The interface's stylesheets are packed into one cascade (styles/order.ts): a class or an animation of the notice that
// another stylesheet also defines gets that one's rules too. composer.css once had `.hx-drop` (its file-drop hint) and
// laid `position: absolute; inset: 0`, a dashed border and `pointer-events: none` over the notice's drops.
const STYLES = [
  fileURLToPath(new URL('../../styles/', import.meta.url)),
  fileURLToPath(new URL('../../../styles/', import.meta.url)),
];
const OWN = 'smart-notice.css';

const sheets = STYLES.flatMap((dir) =>
  readdirSync(dir)
    .filter((name) => name.endsWith('.css'))
    .map((name) => ({ name, css: readFileSync(dir + name, 'utf8') })),
);
const own = sheets.find((s) => s.name === OWN)?.css ?? '';
const others = sheets.filter((s) => s.name !== OWN);

const unique = (found: Iterable<string>) => [...new Set(found)];
const classesOf = (css: string) => unique(css.match(/\.hx-sn[a-z0-9_-]*/g) ?? []);
const animationsOf = (css: string) =>
  unique([...css.matchAll(/@keyframes\s+([a-z0-9-]+)/g)].map((m) => m[1]));

describe('the smart notice stylesheet', () => {
  it('is found with its classes', () => {
    expect(classesOf(own)).toContain('.hx-sn-drop');
  });

  it('shares no class with another stylesheet', () => {
    const shared = others.flatMap(({ name, css }) => {
      const theirs = new Set(classesOf(css));
      return classesOf(own)
        .filter((cls) => theirs.has(cls))
        .map((cls) => `${cls} in ${name}`);
    });
    expect(shared).toEqual([]);
  });

  it('shares no animation name with another stylesheet', () => {
    const shared = others.flatMap(({ name, css }) => {
      const theirs = new Set(animationsOf(css));
      return animationsOf(own)
        .filter((anim) => theirs.has(anim))
        .map((anim) => `${anim} in ${name}`);
    });
    expect(shared).toEqual([]);
  });
});
