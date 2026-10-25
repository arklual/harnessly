import { describe, expect, it } from 'vitest';
import { planTabMove, TAB } from '@/next/features/shell/tab-order';
import { EDGE } from '@/next/ui/use-drag';

const IDS = ['a', 'b', 'c', 'd'];
const plan = (source: string, target: string, edge: 'before' | 'after') =>
  planTabMove(IDS, { kind: TAB, id: source }, { kind: TAB, id: target, edge });

describe('planTabMove', () => {
  it('puts a tab before another one', () => {
    expect(plan('d', 'b', EDGE.before)).toEqual({ id: 'd', index: 1 });
    expect(plan('a', 'c', EDGE.before)).toEqual({ id: 'a', index: 1 });
  });

  it('puts a tab after another one', () => {
    expect(plan('a', 'c', EDGE.after)).toEqual({ id: 'a', index: 2 });
    expect(plan('d', 'a', EDGE.after)).toEqual({ id: 'd', index: 1 });
  });

  it('goes to the ends', () => {
    expect(plan('c', 'a', EDGE.before)).toEqual({ id: 'c', index: 0 });
    expect(plan('a', 'd', EDGE.after)).toEqual({ id: 'a', index: 3 });
  });

  it('is nothing where the tab stays', () => {
    expect(plan('b', 'b', EDGE.before)).toBeNull();
    expect(plan('b', 'c', EDGE.before)).toBeNull();
    expect(plan('b', 'a', EDGE.after)).toBeNull();
  });

  it('is nothing for a tab the strip does not have', () => {
    expect(plan('x', 'a', EDGE.before)).toBeNull();
    expect(plan('a', 'x', EDGE.before)).toBeNull();
  });
});
