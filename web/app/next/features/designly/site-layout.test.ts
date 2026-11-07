import { describe, expect, it } from 'vitest';
import {
  MAX_LAYOUTS,
  parseLayout,
  parseLayouts,
  withLayout,
  type ISiteLayout,
} from '@/next/features/designly/site-layout';

const layout: ISiteLayout = {
  mode: 'chat',
  min: false,
  dock: 'right',
  chatW: 420,
  float: null,
  opened: 'chat',
  chosen: true,
};

describe('site layout', () => {
  it('reads back what it kept', () => {
    const kept = withLayout(new Map(), 'm/d1', layout);
    expect(parseLayouts(JSON.stringify(Object.fromEntries(kept))).get('m/d1')).toEqual(layout);
  });

  // a place kept before the person placed the chat by hand is not their choice.
  it('reads an older layout without the mark as not chosen', () => {
    const { chosen: _chosen, ...older } = layout;
    expect(parseLayout(older)?.chosen).toBe(false);
  });

  it('brings picking notes back as the field', () => {
    expect(parseLayout({ ...layout, mode: 'notes' })?.mode).toBe('island');
  });

  it('drops what is not a layout', () => {
    expect(parseLayout({ ...layout, dock: 'top' })).toBeNull();
    expect(parseLayout({ ...layout, float: { x: 1 } })).toBeNull();
    expect(parseLayouts('{broken').size).toBe(0);
  });

  it('forgets the least recent beyond the cap', () => {
    let kept = new Map<string, ISiteLayout>();
    for (let i = 0; i <= MAX_LAYOUTS; i += 1) kept = withLayout(kept, `d${i}`, layout);
    kept = withLayout(kept, 'd1', layout);
    expect(kept.size).toBe(MAX_LAYOUTS);
    expect(kept.has('d0')).toBe(false);
    expect([...kept.keys()].at(-1)).toBe('d1');
  });
});
