import { describe, expect, it } from 'vitest';
import type { IPickerAgent } from '@/next/features/composer/agents';
import { settingsChoice, startChoice } from '@/next/features/composer/start-choice';
import type { AgentModel } from '@/next/protocol/v2';

const model = (id: string): AgentModel => ({
  id,
  label: id,
  hint: '',
  efforts: ['low', 'medium', 'high'],
  defaultEffort: 'high',
});

const claude: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  models: [model('fable'), model('opus'), model('sonnet')],
  canSwitch: true,
  available: true,
  status: 'ready',
};
const codex: IPickerAgent = { ...claude, provider: 'codex', models: [model('gpt-6')] };

const NONE = { claude: null, codex: null };

describe('startChoice', () => {
  it('starts on the settings default, not on the first model', () => {
    const settings = {
      ...NONE,
      claude: [
        { key: 'model', value: 'opus' },
        { key: 'effort', value: 'medium' },
      ] as const,
    };
    expect(startChoice({ ready: [claude], picked: null, last: null, settings })).toEqual({
      provider: 'claude',
      model: 'opus',
      effort: 'medium',
      ultracode: false,
    });
  });

  it('falls back to the last pick when the settings name no model', () => {
    const last = { provider: 'claude', model: 'sonnet', effort: 'low', ultracode: false } as const;
    expect(startChoice({ ready: [claude], picked: null, last, settings: NONE })).toEqual(last);
  });

  it('takes the first model only when nothing else is known', () => {
    expect(startChoice({ ready: [claude], picked: null, last: null, settings: NONE })?.model).toBe('fable');
  });

  it('keeps the pick made on the page', () => {
    const picked = { provider: 'codex', model: 'gpt-6', effort: null, ultracode: false } as const;
    const settings = { ...NONE, claude: [{ key: 'model', value: 'opus' }] as const };
    expect(startChoice({ ready: [claude, codex], picked, last: null, settings })).toEqual(picked);
  });

  it('starts on the agent of the last pick', () => {
    const last = { provider: 'codex', model: 'gpt-6', effort: null, ultracode: false } as const;
    expect(startChoice({ ready: [claude, codex], picked: null, last, settings: NONE })?.provider).toBe(
      'codex',
    );
  });
});

describe('settingsChoice', () => {
  it('reads an alias with a context suffix and a full id', () => {
    expect(settingsChoice(claude, [{ key: 'model', value: 'opus[1m]' }])?.model).toBe('opus');
    expect(settingsChoice(claude, [{ key: 'model', value: 'claude-sonnet-5-5' }])?.model).toBe('sonnet');
  });

  it('gives the model its own depth when the settings depth does not fit', () => {
    expect(
      settingsChoice(claude, [
        { key: 'model', value: 'opus' },
        { key: 'effort', value: 'max' },
      ])?.effort,
    ).toBe('high');
  });
});

describe('ultracode of a new chat', () => {
  const ultra: IPickerAgent = { ...claude, models: [{ ...model('opus'), ultracode: true }, model('haiku')] };

  it('is on when the settings say so and the model runs it', () => {
    const on = [
      { key: 'model', value: 'opus' },
      { key: 'ultracode', value: true },
    ] as const;
    expect(settingsChoice(ultra, on)?.ultracode).toBe(true);
    expect(settingsChoice(ultra, [{ key: 'model', value: 'haiku' }, on[1]])?.ultracode).toBe(false);
    expect(settingsChoice(ultra, [on[0]])?.ultracode).toBe(false);
  });

  it('comes back with the last pick only on a model that runs it', () => {
    const last = { provider: 'claude', model: 'opus', effort: 'low', ultracode: true } as const;
    expect(startChoice({ ready: [ultra], picked: null, last, settings: NONE })?.ultracode).toBe(true);
    const haiku = { ...last, model: 'haiku' } as const;
    expect(startChoice({ ready: [ultra], picked: null, last: haiku, settings: NONE })?.ultracode).toBe(false);
  });
});
