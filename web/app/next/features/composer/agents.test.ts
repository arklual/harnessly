import { beforeEach, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import {
  effortLabel,
  modelLine,
  modelName,
  modelOf,
  pickerAgents,
  removedKeyOf,
  signInNeeded,
  unavailableNote,
  type IPickerAgent,
} from './agents';
import type { MachineAgent } from '@/next/protocol/v2';

beforeEach(() => pinLocale('en'));

describe('configured profiles use their own credentials', () => {
  const machine = (status: MachineAgent['status'], configured = true): MachineAgent => ({
    provider: 'claude',
    version: null,
    status,
    models: [],
    canSwitchModel: true,
    profiles: [
      {
        id: 'gpt',
        label: 'GPT subscription',
        vendor: 'gpt',
        configured,
        models: [{ id: 'gpt-test', label: 'GPT', hint: '', efforts: ['low'], defaultEffort: 'low' }],
      },
    ],
  });

  it('offers a configured GPT profile without an Anthropic login', () => {
    const choices = pickerAgents([machine('login-required')]);
    expect(choices.find((a) => a.profile === 'gpt')).toMatchObject({ available: true, status: 'ready' });
    expect(choices.find((a) => !a.profile)).toMatchObject({ available: false, status: 'login-required' });
    expect(signInNeeded(choices)).toBe(false);
  });

  it('still requires the underlying CLI to be installed', () => {
    expect(pickerAgents([machine('missing')]).find((a) => a.profile === 'gpt')).toMatchObject({
      available: false,
      status: 'missing',
    });
  });

  it('does not offer a profile without its own credentials', () => {
    expect(pickerAgents([machine('ready', false)]).some((a) => a.profile === 'gpt')).toBe(false);
  });
});

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

describe('a machine where no agent is signed in', () => {
  const row = (provider: string, status: string, available = status === 'ready') =>
    ({ provider, name: provider, status, available }) as unknown as IPickerAgent;
  it('asks for a sign-in when an installed agent is signed out and none else is ready', () => {
    expect(signInNeeded([row('claude', 'login-required'), row('codex', 'login-required')])).toBe(true);
    expect(signInNeeded([row('claude', 'login-required'), row('codex', 'missing')])).toBe(true);
  });
  it('does not ask while one agent is signed in, the other being signed out', () => {
    expect(signInNeeded([row('claude', 'login-required'), row('codex', 'ready')])).toBe(false);
  });
  it('does not ask for a sign-in where nothing is installed', () => {
    expect(signInNeeded([row('claude', 'missing'), row('codex', 'missing')])).toBe(false);
    expect(signInNeeded([])).toBe(false);
  });
});

describe('a chat of a vendor whose key was removed', () => {
  const machine = (configured: boolean) =>
    [{ provider: 'claude', profiles: [{ id: 'kimi', label: 'Kimi', configured }] }] as unknown as Parameters<
      typeof removedKeyOf
    >[0];

  it('names the vendor while its profile is listed without a key', () => {
    expect(removedKeyOf(machine(false), 'claude', 'kimi')).toBe('Kimi');
  });

  it('says nothing while the key is there, for Claude itself or for an unknown profile', () => {
    expect(removedKeyOf(machine(true), 'claude', 'kimi')).toBeNull();
    expect(removedKeyOf(machine(false), 'claude', null)).toBeNull();
    expect(removedKeyOf(machine(false), 'claude', 'glm')).toBeNull();
  });
});

describe('the picker names a model by how it runs (HRN-1434)', () => {
  const model = (id: string, label: string) => ({ id, label, hint: '', efforts: [], defaultEffort: null });
  const machine: MachineAgent = {
    provider: 'claude',
    version: null,
    status: 'ready',
    models: [model('opus', 'Opus')],
    canSwitchModel: true,
    profiles: [
      {
        id: 'gpt',
        label: 'GPT · ChatGPT plan',
        vendor: 'gpt',
        configured: true,
        models: [model('g', 'GPT-5')],
      },
      { id: 'glm', label: 'GLM · Z.ai', vendor: 'glm', configured: true, models: [model('m', 'GLM 5.3')] },
      { id: 'kimi', label: 'Kimi · platform', vendor: 'kimi', configured: true, models: [model('k', 'K3')] },
      { id: 'kimi-code', label: 'Kimi Code', vendor: 'kimi', configured: true, models: [model('k', 'K3')] },
    ],
  } as MachineAgent;
  const of = (profile: string | null) =>
    pickerAgents([machine]).find((a) => (a.profile ?? null) === profile)!;

  it('says Claude’s own models by name alone', () => {
    expect(modelLine(of(null), 'opus')).toBe('Opus');
  });

  it('says GPT goes through Codex into Claude Code, the others through Claude Code', () => {
    expect(modelLine(of('gpt'), 'g')).toBe('GPT-5 · Codex via Claude Code');
    expect(modelLine(of('glm'), 'm')).toBe('GLM 5.3 · via Claude Code');
  });

  it('names a group’s tab by its vendor unless two would read the same', () => {
    expect(pickerAgents([machine]).map((a) => a.short)).toEqual([
      'Claude',
      'GPT',
      'GLM',
      'Kimi',
      'Kimi Code',
    ]);
  });
});

describe('Antigravity native agent', () => {
  const native: MachineAgent = {
    provider: 'antigravity',
    version: '1.0',
    status: 'ready',
    canSwitchModel: false,
    models: [{ id: 'machine-model', label: 'Machine model', hint: '', efforts: [], defaultEffort: null }],
  };

  it('offers only the models reported by the machine, marked experimental', () => {
    const [picked] = pickerAgents([native]);
    expect(picked).toMatchObject({
      provider: 'antigravity',
      name: 'Antigravity',
      canSwitch: false,
      available: true,
    });
    expect(picked.short).toContain('experimental');
    expect(picked.models.map((m) => m.id)).toEqual(['machine-model']);
    expect(pickerAgents([{ ...native, models: [] }])[0].available).toBe(false);
    // Listing models is not proof of a runnable native session (onboarding/eligibility may still block it).
    expect(pickerAgents([{ ...native, status: 'login-required' }])[0].available).toBe(false);
    expect(pickerAgents([{ ...native, status: 'missing' }])[0].available).toBe(false);
  });

  it('does not offer Claude controls or profile groups even if advertised accidentally', () => {
    const picked = pickerAgents([
      {
        ...native,
        canSwitchModel: true,
        models: native.models.map((model) => ({ ...model, fast: true, ultracode: true })),
        profiles: [{ id: 'test', label: 'Test', vendor: 'gpt', configured: true, models: native.models }],
      },
    ]);
    expect(picked).toHaveLength(1);
    expect(picked[0].canSwitch).toBe(false);
    expect(picked[0].models[0]).toMatchObject({ fast: false, ultracode: false });
  });

  it('gives the official executable login command', () => {
    const [picked] = pickerAgents([{ ...native, status: 'login-required' }]);
    expect(unavailableNote(picked)).toContain('agy acp --login');
  });
});
