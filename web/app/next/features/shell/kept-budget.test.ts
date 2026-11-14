import { describe, expect, it, vi } from 'vitest';
import { keptHiddenMax, makeKeptBudget, overBudget } from '@/next/features/shell/kept-budget';

describe('kept budget', () => {
  it('keeps the most recent hidden ones, whoever owns them, and never what is on screen', () => {
    const over = overBudget(
      [
        { key: 'screen:a', shown: true, at: 1 },
        { key: 'screen:b', shown: false, at: 10 },
        { key: 'pane:c', shown: false, at: 30 },
        { key: 'pane:d', shown: false, at: 20 },
      ],
      2,
    );
    expect([...over]).toEqual(['screen:b']);
  });

  it('one budget across owners: a pane browser hidden later pushes the oldest screen out', () => {
    const budget = makeKeptBudget(1);
    const told = vi.fn();
    budget.subscribe(told);
    budget.report('screens', [
      { key: 'screen:a', shown: true, at: 5 },
      { key: 'screen:b', shown: false, at: 3 },
    ]);
    expect(budget.allows('screen:b')).toBe(true);
    budget.report('pane', [{ key: 'pane:x', shown: false, at: 4 }]);
    expect(budget.allows('screen:b')).toBe(false);
    expect(budget.allows('pane:x')).toBe(true);
    expect(told).toHaveBeenCalledTimes(1);
    // The same set again tells nobody.
    budget.report('pane', [{ key: 'pane:x', shown: false, at: 4 }]);
    expect(told).toHaveBeenCalledTimes(1);
  });

  it('out of sight: the device count less the one on screen', () => {
    expect(keptHiddenMax(undefined)).toBe(4);
    expect(keptHiddenMax(4)).toBe(2);
    expect(keptHiddenMax(2)).toBe(1);
  });
});
