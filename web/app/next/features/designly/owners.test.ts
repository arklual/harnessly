import { describe, expect, it } from 'vitest';
import { DESIGN_SCOPE, initialOf, inScope } from '@/next/features/designly/owners';

const me = { name: 'Пётр', me: true };
const other = { name: 'Олег', me: false };

describe('inScope', () => {
  it('shows every card under «Все», the owner known or not', () => {
    expect([me, other, null].map((o) => inScope(DESIGN_SCOPE.all, o))).toEqual([true, true, true]);
  });

  it('shows only the person’s own under «Мои»; an unknown owner is not theirs', () => {
    expect([me, other, null].map((o) => inScope(DESIGN_SCOPE.mine, o))).toEqual([true, false, false]);
  });

  it('shows only the teammates’ under «Общие»; an unknown owner is not shared either', () => {
    expect([me, other, null].map((o) => inScope(DESIGN_SCOPE.shared, o))).toEqual([false, true, false]);
  });
});

describe('initialOf', () => {
  it('is the first letter, upper case, whole for a letter outside the basic plane', () => {
    expect(initialOf('пётр')).toBe('П');
    expect(initialOf('  Oleg')).toBe('O');
    expect(initialOf('𝒜da')).toBe('𝒜');
    expect(initialOf('  ')).toBe('');
  });
});
