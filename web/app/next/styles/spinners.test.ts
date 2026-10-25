import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const DIR = __dirname;
const SPIN = /([^{}]+)\{[^{}]*animation:\s*hx-spin\b[^{}]*\}/g;

const read = (name: string) => readFileSync(join(DIR, name), 'utf8');
const sheets = readdirSync(DIR).filter((name) => name.endsWith('.css'));

const norm = (selector: string) => selector.replace(/\s+/g, ' ').trim();

const spinnerSelectors = () => {
  const found = new Set<string>();
  for (const name of sheets) {
    for (const match of read(name).matchAll(SPIN)) {
      const own = match[1].split('*/').pop() ?? '';
      for (const part of own.split(',')) found.add(norm(part));
    }
  }
  return [...found];
};

describe('spinners', () => {
  it('every rule that runs hx-spin is kept turning under reduced motion', () => {
    const motion = read('motion.css').replace(/\s+/g, ' ');
    const missing = spinnerSelectors().filter((selector) => !motion.includes(selector));
    expect(missing).toEqual([]);
  });

  it('the reduced-motion rule slows spinners instead of stopping them', () => {
    const motion = read('motion.css');
    expect(motion).toContain('animation-iteration-count: infinite !important');
  });

  it('hx-spin keyframes are never empty', () => {
    for (const name of sheets) {
      for (const match of read(name).matchAll(/@keyframes hx-spin\s*\{([^]*?)\n\}/g)) {
        expect(match[1]).toContain('rotate(360deg)');
      }
    }
  });
});
