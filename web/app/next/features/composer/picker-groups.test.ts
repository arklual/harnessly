import { describe, expect, it } from 'vitest';
import type { IPickerAgent } from '@/next/features/composer/agents';
import { modelHint, OPENAI, pickerGroups } from '@/next/features/composer/picker-groups';

const agent = (provider: string, extra: Partial<IPickerAgent> = {}): IPickerAgent =>
  ({
    provider,
    name: provider,
    short: provider,
    models: [],
    canSwitch: true,
    available: true,
    status: 'ready',
    ...extra,
  }) as IPickerAgent;
const keyOf = (a: IPickerAgent) => a.profile ?? a.provider;

describe('the picker’s tabs (15.11: «почему стало 4»)', () => {
  it('are vendors: Claude · OpenAI · others; OpenAI holds harnessly’s agent, Codex and the chat', () => {
    const agents = [
      agent('claude'),
      agent('codex'),
      agent('claude', { profile: 'gpt', vendor: 'gpt' }),
      agent('claude', { profile: 'kimi', vendor: 'kimi' }),
    ];
    const groups = pickerGroups(agents, keyOf, '~chat');
    expect(groups.map((g) => g.key)).toEqual(['claude', OPENAI, 'kimi']);
    expect(groups[1].members.map((m) => m.mode)).toEqual(['harnessly', 'codex', 'chat']);
    expect(groups[1].members[0].key).toBe('gpt');
  });
  it('has no OpenAI tab when nothing of OpenAI is there', () => {
    expect(pickerGroups([agent('claude')], keyOf, null).map((g) => g.key)).toEqual(['claude']);
  });
});

describe('model hints', () => {
  it('are Russian or none: no English text from the vendor', () => {
    expect(modelHint('gpt-5.1-codex-max', 'Older generation workhorse model')).toBe('Самая сильная для кода');
    expect(modelHint('gpt-5.1-codex-mini', 'Cheaper, faster')).toBe('Быстрая и лёгкая');
    expect(modelHint('kimi-k2.7-code', 'Для кода и фоновой работы')).toBe('Для кода и фоновой работы');
    expect(modelHint('weird-model', 'Some English line')).toBe('');
    for (const id of ['gpt-5.2', 'o4', 'claude-opus-5-5'])
      expect(modelHint(id, 'English')).not.toMatch(/[a-z]{3}/i);
  });
});
