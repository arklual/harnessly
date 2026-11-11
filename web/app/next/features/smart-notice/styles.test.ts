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

  it("contrasting by default (black on the light theme, light on the dark), the theme's own by the person's choice", () => {
    expect(tokens).toContain(BLACK);
    expect(tokens).toContain(LIGHT);
  });

  it('is drawn with the --hx-toast-* tokens: the black card as before', () => {
    expect(own).toMatch(/\.hx-sn\s*\{\s*--sn-bg: var\(--hx-toast-bg\)/);
    // Nothing is drawn white or black past the tokens.
    expect(own).not.toMatch(/rgba\(255, 255, 255/);
    const light = tokenBlock(BLACK);
    expect(light['--hx-toast-bg']).toBe('#000');
    expect(light['--hx-toast-fg']).toBe('#fff');
  });

  it('reads well on the light card of the dark theme: words 4.5:1 and more, the faint ones too', () => {
    const dark = tokenBlock(LIGHT);
    const bg = rgbOf(dark['--hx-toast-bg']);
    expect(luminance(bg)).toBeGreaterThan(0.8);
    for (const name of [
      '--hx-toast-fg',
      '--hx-toast-fg2',
      '--hx-toast-fg3',
      '--hx-toast-warn',
      '--hx-toast-error',
    ])
      expect(contrast(over(dark[name], bg), bg), name).toBeGreaterThanOrEqual(4.5);
    // A button: the card's words on its fill, the danger's ink on the danger.
    const fill = over(dark['--hx-toast-fill2'], bg);
    expect(contrast(over(dark['--hx-toast-fg'], fill), fill)).toBeGreaterThanOrEqual(4.5);
    const danger = rgbOf(dark['--hx-toast-danger']);
    expect(contrast(rgbOf(dark['--hx-toast-danger-ink']), danger)).toBeGreaterThanOrEqual(4.5);
  });
});

/** The black card: the light theme by default, the dark one when the person wants the theme's own. */
const BLACK = ":root,\nhtml[data-theme='dark'][data-notices='theme'] {";
/** The light card: the dark theme by default, the light one when the person wants the theme's own. */
const LIGHT =
  "html[data-theme='dark']:not([data-notices='theme']),\nhtml:not([data-theme='dark'])[data-notices='theme'] {";

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

type TRgba = [number, number, number, number];

function rgbOf(value: string): TRgba {
  const hex = value.match(/^#([0-9a-f]{3}|[0-9a-f]{6})$/i)?.[1];
  if (hex) {
    const full = hex.length === 3 ? [...hex].map((c) => c + c).join('') : hex;
    return [0, 2, 4].map((i) => Number.parseInt(full.slice(i, i + 2), 16)).concat(1) as TRgba;
  }
  const rgba = value.match(/^rgba\((\d+), (\d+), (\d+), ([0-9.]+)\)$/);
  if (!rgba) throw new Error(`not a colour: ${value}`);
  return [Number(rgba[1]), Number(rgba[2]), Number(rgba[3]), Number(rgba[4])];
}

/** `value` laid over the opaque `base`. */
function over(value: string, base: TRgba): TRgba {
  const [r, g, b, a] = rgbOf(value);
  return [r * a + base[0] * (1 - a), g * a + base[1] * (1 - a), b * a + base[2] * (1 - a), 1];
}

function luminance([r, g, b]: TRgba): number {
  const lin = (c: number) => {
    const v = c / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(a: TRgba, b: TRgba): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}
