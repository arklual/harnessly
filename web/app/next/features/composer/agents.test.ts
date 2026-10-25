import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { effortLabel, modelName, modelOf, unavailableNote, type IPickerAgent } from './agents';

beforeAll(() => pinLocale('en'));

const agent = {
  provider: 'claude',
  name: 'Claude Code',
  models: [
    { id: 'opus', label: 'Opus', efforts: ['low', 'high'], defaultEffort: 'high' },
    { id: 'sonnet', label: 'Sonnet', efforts: ['low', 'high'], defaultEffort: 'high' },
  ],
} as unknown as IPickerAgent;

describe('the model a chat names', () => {
  it('is the listed one by its id', () => {
    expect(modelName(agent, 'opus')).toBe('Opus');
  });
  it('is a full id as people say it, and its family for the depth', () => {
    expect(modelName(agent, 'claude-opus-5-5')).toBe('Opus 5.5');
    expect(modelName(agent, 'claude-sonnet-5-5-20261001')).toBe('Sonnet 5.5');
    expect(modelOf(agent, 'claude-opus-5-5')?.id).toBe('opus');
  });
  it('is the agent when no one said', () => {
    expect(modelName(agent, null)).toBe('Claude Code');
    expect(modelName(agent, 'gpt-x')).toBe('gpt-x');
  });
});

describe('an agent that cannot start a chat', () => {
  const codex = { provider: 'codex', name: 'Codex', status: 'login-required' } as unknown as IPickerAgent;
  it('says how to sign in', () => {
    expect(unavailableNote(codex)).toBe('Sign in to Codex on this machine: run codex login in its terminal');
  });
  it('says it is not installed', () => {
    expect(unavailableNote({ ...codex, status: 'missing' })).toBe('Codex is not installed on this machine');
  });
  it('names the ultra depth', () => {
    expect(effortLabel('ultra')).toBe('Ultra');
  });
});
