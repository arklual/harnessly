import { describe, expect, it } from 'vitest';
import { firstAsk } from '@/next/features/app-views/asks';
import type { IAppAsk } from '@/next/features/app-views/api';

const ask = (id: string, createdAt: number): IAppAsk => ({
  id,
  app: 'a.one',
  appName: 'Кешбэки',
  question: 'Что?',
  options: ['да', 'нет'],
  createdAt,
});

describe('firstAsk', () => {
  it('is the oldest ask not answered here yet', () => {
    const asks = [ask('b', 20), ask('a', 10)];
    expect(firstAsk(asks, new Set())?.id).toBe('a');
    expect(firstAsk(asks, new Set(['a']))?.id).toBe('b');
    expect(firstAsk(asks, new Set(['a', 'b']))).toBeNull();
    expect(firstAsk([], new Set())).toBeNull();
  });
});
