import { describe, expect, it } from 'vitest';

import { AUTO, type Builder, builderOptions, fitting, loadBuilders } from '@/next/features/builders/builders';

const b = (over: Partial<Builder>): Builder => ({
  id: 'b1',
  name: 'render-1',
  caps: ['blender', 'gpu'],
  slots: 2,
  running: 1,
  waiting: 0,
  online: true,
  ready: true,
  ...over,
});

describe('builders for a picker', () => {
  it('offers auto first, then the builders that carry the labels, with their load', () => {
    const pool = {
      hub: true,
      builders: [
        b({}),
        b({ id: 'b2', name: 'ffmpeg-1', caps: ['ffmpeg'] }),
        b({ id: 'b3', name: 'off', ready: false, note: 'offline' }),
      ],
    };
    const opts = builderOptions(pool, ['blender']);
    expect(opts.map((o) => o.value)).toEqual([AUTO, 'b1', 'b3']);
    expect(opts[0].label).toContain('(1)');
    expect(opts[1].label).toBe('render-1 · 1 из 2 · blender, gpu');
    expect(opts[2]).toMatchObject({ disabled: true });
    expect(opts[2].label).toContain('offline');
  });

  it('says auto runs here when there is no builder', () => {
    expect(builderOptions({ hub: false, builders: [] })[0].label).toContain('на этой машине');
    expect(fitting([b({})], ['gpu', 'Blender'])).toHaveLength(1);
  });

  it('reads the pool, and an unavailable runtime route is an empty pool', async () => {
    const ok = (async () => new Response(JSON.stringify({ hub: true, builders: [b({})] }))) as typeof fetch;
    expect((await loadBuilders(ok)).builders).toHaveLength(1);
    const bad = (async () => new Response('', { status: 404 })) as typeof fetch;
    expect(await loadBuilders(bad)).toEqual({ hub: false, builders: [] });
    const down = (async () => {
      throw new Error('x');
    }) as typeof fetch;
    expect(await loadBuilders(down)).toEqual({ hub: false, builders: [] });
  });
});
