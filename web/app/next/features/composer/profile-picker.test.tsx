// @vitest-environment jsdom
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { firstChoice, pickerAgentOf, pickerAgents, withModel } from '@/next/features/composer/agents';
import ModelPicker from '@/next/features/composer/model-picker';
import { choiceKey } from '@/next/features/composer/multi-model';
import { startChoice } from '@/next/features/composer/start-choice';
import type { MachineAgent } from '@/next/protocol/v2';

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const model = (id: string, label = id) => ({ id, label, hint: '', efforts: [], defaultEffort: null });

const claude = {
  provider: 'claude',
  version: '2.1.310',
  status: 'ready',
  canSwitchModel: true,
  models: [model('opus', 'Opus 5.5')],
  profiles: [
    {
      id: 'kimi',
      label: 'Kimi',
      vendor: 'kimi',
      configured: true,
      models: [model('kimi-k3.1-pro', 'K3.1 Pro')],
    },
    { id: 'glm', label: 'GLM', vendor: 'glm', configured: false, models: [model('glm-5.3')] },
  ],
} as unknown as MachineAgent;
const codex = {
  provider: 'codex',
  version: '0.152.0',
  status: 'ready',
  canSwitchModel: true,
  models: [model('gpt-6', 'GPT-6')],
  profiles: [
    {
      id: 'xai',
      label: 'Grok',
      vendor: 'grok',
      configured: true,
      models: [model('grok-build-0.1', 'Grok Build')],
    },
  ],
} as unknown as MachineAgent;

describe('profile groups', () => {
  it('lists Claude and Codex first, then a group per connected profile; a profile without a key is not listed', () => {
    const keys = pickerAgents([claude, codex]).map((a) => a.profile ?? a.provider);
    expect(keys).toEqual(['claude', 'codex', 'kimi', 'xai']);
  });

  it('adds nothing for a machine that reports no profiles', () => {
    const plain = { ...claude, profiles: undefined } as unknown as MachineAgent;
    expect(pickerAgents([plain, codex]).map((a) => a.profile ?? a.provider)).toEqual([
      'claude',
      'codex',
      'xai',
    ]);
  });

  it('keeps a plain Claude choice free of any profile', () => {
    const [own] = pickerAgents([claude]);
    expect(firstChoice(own)).toEqual({ provider: 'claude', model: 'opus', effort: null, ultracode: false });
  });

  it('puts the profile into a choice made in its group, and takes it out when a Claude model is chosen', () => {
    const [own, , kimi] = pickerAgents([claude, codex]);
    const inKimi = withModel(firstChoice(own), kimi, kimi.models[0]);
    expect(inKimi).toMatchObject({ provider: 'claude', profile: 'kimi', model: 'kimi-k3.1-pro' });
    const back = withModel(inKimi, own, own.models[0]);
    expect(back.profile).toBeUndefined();
  });

  it('tells a model of a profile from the same id on its agent, in a set of several models', () => {
    expect(choiceKey({ provider: 'claude', profile: 'kimi', model: 'x' })).not.toBe(
      choiceKey({ provider: 'claude', model: 'x' }),
    );
  });

  it('finds the group of a chat by its profile', () => {
    expect(pickerAgentOf([claude, codex], 'claude', 'kimi')?.short).toBe('Kimi');
    expect(pickerAgentOf([claude, codex], 'claude', 'glm')).toBeNull();
    expect(pickerAgentOf([claude, codex], 'claude')?.short).toBe('Claude');
  });

  it('starts a new chat on the last pick in a profile group while it is connected, else on the first agent', () => {
    const ready = pickerAgents([claude, codex]);
    const last = {
      provider: 'claude',
      profile: 'kimi',
      model: 'kimi-k3.1-pro',
      effort: null,
      ultracode: false,
    } as const;
    const none = { claude: null, codex: null };
    expect(startChoice({ ready, picked: null, last, settings: none })).toEqual(last);
    const lost = ready.filter((a) => a.profile !== 'kimi');
    expect(startChoice({ ready: lost, picked: null, last, settings: none })?.profile).toBeUndefined();
  });
});

describe('the picker with profile groups', () => {
  const open = (onChange = vi.fn()) => {
    const agents = pickerAgents([claude, codex]);
    const view = render(
      <ModelPicker
        value={{ provider: 'claude', model: 'opus', effort: null, ultracode: false }}
        agents={agents}
        locked={false}
        onChange={onChange}
      />,
    );
    const pill = view.container.querySelector<HTMLButtonElement>('.hx-model');
    if (!pill) throw new Error('no pill');
    act(() => pill.click());
    return { view, onChange };
  };

  it('offers a segment per group with the vendor name, and the models of the group in its list', () => {
    const { view } = open();
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-picker__center')?.click());
    const segs = [...view.container.querySelectorAll('.hx-seg__item')].map((el) => el.textContent);
    expect(segs).toEqual(['Claude', 'Codex', 'Kimi', 'Grok']);
    const kimi = view.container.querySelectorAll<HTMLButtonElement>('.hx-seg__item')[2];
    act(() => kimi.click());
    const rows = view.container.querySelectorAll<HTMLElement>('.hx-options[data-active="true"] .hx-option');
    expect([...rows].map((r) => r.textContent)).toEqual(['K3.1 Pro']);
  });

  it('chooses a model of a group with its profile', () => {
    const { view, onChange } = open();
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-picker__center')?.click());
    act(() => view.container.querySelectorAll<HTMLButtonElement>('.hx-seg__item')[2].click());
    act(() =>
      view.container.querySelector<HTMLButtonElement>('.hx-options[data-active="true"] .hx-option')?.click(),
    );
    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ provider: 'claude', profile: 'kimi', model: 'kimi-k3.1-pro' }),
    );
  });
});
