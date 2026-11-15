import { describe, expect, it } from 'vitest';
import { reloadOnce, STALE_RELOAD_GAP_MS } from '@/lib/stale-chunks';

const memory = () => {
  const m = new Map<string, string>();
  return { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
};

describe('reloadOnce (HRN-1471)', () => {
  it('reloads once, and again only after the gap', () => {
    const store = memory();
    expect(reloadOnce(store, 1000)).toBe(true);
    expect(reloadOnce(store, 1000 + STALE_RELOAD_GAP_MS - 1)).toBe(false);
    expect(reloadOnce(store, 1000 + STALE_RELOAD_GAP_MS)).toBe(true);
  });
  it('does not reload when nothing can be kept', () => {
    expect(reloadOnce(null, 1000)).toBe(false);
    const broken = {
      getItem: () => {
        throw new Error('denied');
      },
      setItem: () => undefined,
    };
    expect(reloadOnce(broken, 1000)).toBe(false);
  });
});
