import { describe, expect, it } from 'vitest';
import type { IModelChoice } from '@/next/features/composer/agents';
import { choiceKey, launchPrompt, MULTI_MAX, toggleChoice } from '@/next/features/composer/multi-model';

const pick = (model: string, effort: IModelChoice['effort'] = null): IModelChoice => ({
  provider: 'claude',
  model,
  effort,
  ultracode: false,
});

describe('toggleChoice', () => {
  it('adds a model and takes it out again', () => {
    const one = [pick('opus')];
    const two = toggleChoice(one, pick('sonnet'));
    expect(two.map(choiceKey)).toEqual(['claude:opus', 'claude:sonnet']);
    expect(toggleChoice(two, pick('sonnet', 'high')).map(choiceKey)).toEqual(['claude:opus']);
  });

  it('never takes out the last model', () => {
    expect(toggleChoice([pick('opus')], pick('opus')).map(choiceKey)).toEqual(['claude:opus']);
  });

  it('stops at the cap', () => {
    let set = [pick('m0')];
    for (let i = 1; i < MULTI_MAX + 2; i += 1) set = toggleChoice(set, pick(`m${i}`));
    expect(set).toHaveLength(MULTI_MAX);
  });
});

describe('launchPrompt', () => {
  it('is the first line, cut', () => {
    expect(launchPrompt('  Сделай форму\nи тесты')).toBe('Сделай форму');
    expect(launchPrompt('x'.repeat(300))).toHaveLength(200);
  });
});
