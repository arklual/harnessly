import { describe, expect, it } from 'vitest';
import presetsJson from '@/next/features/looks/presets.json';
import { contrast, ensure, lookTokens } from '@/next/features/looks/tokens';
import {
  NO_LOOKS,
  PRESETS,
  STARTER_LOOK,
  lookFor,
  readLook,
  readLooks,
  withLook,
  MAX_SCOPED,
} from '@/next/features/looks/types';

const oklch = (v: string): [number, number, number] => {
  const m = /oklch\(([\d.]+) ([\d.]+) ([\d.]+)/.exec(v);
  if (!m) throw new Error(v);
  return [Number(m[1]), Number(m[2]), Number(m[3])];
};

describe('looks', () => {
  it('reads every preset', () => {
    expect(PRESETS).toHaveLength((presetsJson as unknown[]).length);
  });

  it('refuses what is not a look and drops what it does not know', () => {
    expect(readLook({ ...STARTER_LOOK, hues: [] })).toBeNull();
    expect(
      readLook({ ...STARTER_LOOK, bg: { kind: 'image', image: 'https://evil.example.com/x.png' } }),
    ).toBeNull();
    expect(readLook({ ...STARTER_LOOK, bg: { kind: 'scene', scene: '<script>' } })).toBeNull();
    const look = readLook({
      ...STARTER_LOOK,
      css: 'body{display:none}',
      parts: { bubble: 'hidden', composer: 'glass' },
    });
    expect(look).not.toBeNull();
    expect(look && 'css' in look).toBe(false);
    expect(look?.parts.bubble).toBeUndefined();
    expect(look?.parts.composer).toBe('glass');
  });

  it('a chat wears its own look, else its folder’s, else the one for all', () => {
    const red = { ...STARTER_LOOK, name: 'red' };
    const blue = { ...STARTER_LOOK, name: 'blue' };
    const all = { ...STARTER_LOOK, name: 'all' };
    let looks = withLook(NO_LOOKS, { k: 'all' }, all);
    looks = withLook(looks, { k: 'folder', id: 'f' }, red);
    looks = withLook(looks, { k: 'chat', id: 'c' }, blue);
    expect(lookFor(looks, 'c', 'f')?.name).toBe('blue');
    expect(lookFor(looks, 'x', 'f')?.name).toBe('red');
    expect(lookFor(looks, 'x', null)?.name).toBe('all');
    expect(lookFor(withLook(looks, { k: 'all' }, null), 'x', null)).toBeNull();
    expect(readLooks(JSON.stringify(looks))).toEqual(looks);
    expect(readLooks('not json')).toEqual(NO_LOOKS);
  });

  it('keeps at most MAX_SCOPED chats', () => {
    let looks = NO_LOOKS;
    for (let i = 0; i < MAX_SCOPED + 5; i++)
      looks = withLook(looks, { k: 'chat', id: `c${i}` }, STARTER_LOOK);
    expect(Object.keys(looks.chats)).toHaveLength(MAX_SCOPED);
    expect(looks.chats.c0).toBeUndefined();
  });

  it('keeps the text readable in every preset, light and dark (WCAG AA)', () => {
    const strong = [
      ...PRESETS.map((p) => p.look),
      { ...STARTER_LOOK, hues: [{ h: 100, s: 100 }], intensity: 100 },
    ];
    for (const look of strong)
      for (const mode of ['light', 'dark'] as const) {
        const tokens = lookTokens(look, mode);
        const app = oklch(tokens['hx-bg-app']);
        expect(contrast(oklch(tokens['hx-fg-2']), app)).toBeGreaterThanOrEqual(4.5);
        expect(contrast(oklch(tokens['hx-fg-3']), app)).toBeGreaterThanOrEqual(3);
        expect(contrast(oklch(tokens['hx-accent']), app)).toBeGreaterThanOrEqual(4.5);
      }
  });

  it('moves a colour until it passes', () => {
    const bg: [number, number, number] = [0.98, 0, 0];
    expect(contrast(ensure([0.9, 0.1, 30], bg, 4.5, 'light'), bg)).toBeGreaterThanOrEqual(4.5);
  });
});
