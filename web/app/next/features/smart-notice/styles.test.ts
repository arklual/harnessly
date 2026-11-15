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
    expect(classesOf(own)).toContain('.hx-sn__plate');
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

  it('lets the pointer through a plate covered by what the field opened', () => {
    const rule = own.match(/\.hx-sn\[data-covered\] \.hx-sn__shape[^{]*\{([^}]*)\}/);
    expect(rule?.[1]).toContain('pointer-events: none');
    expect(own).toMatch(/\.hx-sn\[data-covered\] \.hx-sn__plate/);
    expect(own).toMatch(/\.hx-sn\[data-covered\]\s*\{[^}]*opacity: 0/);
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

  it('puts one away in a short ease-out: the next plate comes forward black, not see-through', () => {
    const rise = own.match(/@keyframes hx-sn-rise\s*\{([\s\S]*?)\n\}/)?.[1] ?? '';
    expect(rise).not.toBe('');
    expect(rise).not.toContain('opacity');
    expect(own).toMatch(
      /\.hx-sn\[data-rise\] \.hx-sn__shape\s*\{[^}]*animation: hx-sn-rise 240ms var\(--hx-ease-out\)/,
    );
    expect(own).toMatch(/--sn-exit-ms: 260ms/);
    expect(own).toMatch(/\.hx-sn__tint\s*\{[^}]*transition: opacity 240ms/);
  });

  it('moves nothing under reduced motion', () => {
    const reduced = own.slice(own.indexOf('@media (prefers-reduced-motion: reduce)'));
    expect(reduced).toMatch(/\.hx-sn\[data-rise\] \.hx-sn__tint\s*\{\s*animation: none/);
    expect(reduced).toMatch(/\.hx-sn__tint\s*\{\s*transition: none/);
  });

  it("is the theme's own card in both themes (HRN-1487): no light card on the dark interface", () => {
    expect(tokens).not.toContain('data-notices');
    const card = tokenBlock(CARD);
    // Every colour of the card is a token of the theme, so it is dark in the dark theme and light in the light one.
    for (const [name, value] of Object.entries(card)) {
      if (name === '--hx-toast-on') continue;
      expect(value, name).toMatch(/^var\(--hx-[a-z0-9-]+\)/);
    }
    expect(card['--hx-toast-bg']).toBe('var(--hx-bg-raised)');
    expect(card['--hx-toast-fg']).toBe('var(--hx-fg-1)');
    expect(tokenBlock(DARK)).toEqual({ '--hx-toast-on': '255 255 255' });
  });

  it('is drawn with the --hx-toast-* tokens', () => {
    expect(own).toMatch(/\.hx-sn\s*\{\s*--sn-bg: var\(--hx-toast-bg\)/);
    // Nothing is drawn white or black past the tokens.
    expect(own).not.toMatch(/rgba\(255, 255, 255/);
  });
});

/** The card's colours: the light theme's by default, each a token of the theme. */
const CARD = ":root,\nhtml[data-theme='light'] {\n  --hx-toast-bg";
/** The dark theme changes only the ink things are laid over the card with. */
const DARK = "html[data-theme='dark'] {\n  --hx-toast-on";

const tokens = readFileSync(fileURLToPath(new URL('../../styles/tokens.css', import.meta.url)), 'utf8');

/** The custom properties of the block that starts with `head`. */
function tokenBlock(head: string): Record<string, string> {
  const at = tokens.indexOf(head);
  expect(at).toBeGreaterThanOrEqual(0);
  const body = tokens.slice(at, tokens.indexOf('}', at));
  return Object.fromEntries(
    [...body.matchAll(/(--[a-z0-9-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]),
  );
}
