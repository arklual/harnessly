// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ModelPicker from '@/next/features/composer/model-picker';
import { withModel, type IModelChoice, type IPickerAgent } from '@/next/features/composer/agents';
import type { AgentModel, Effort } from '@/next/protocol/v2';

// The wide window: the picker is a popover over the field, not the phone's sheet.
beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const levels: Effort[] = ['low', 'medium', 'high', 'xhigh', 'max'];
const opus: AgentModel = {
  id: 'opus',
  label: 'Opus 5.5',
  hint: '',
  efforts: levels,
  defaultEffort: 'medium',
  ultracode: true,
};
const sonnet: AgentModel = {
  id: 'sonnet',
  label: 'Sonnet 5.5',
  hint: '',
  efforts: levels,
  defaultEffort: 'medium',
  ultracode: true,
};
const haiku: AgentModel = { id: 'haiku', label: 'Haiku 4.5', hint: '', efforts: [], defaultEffort: null };

const agent: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  models: [opus, sonnet, haiku],
  canSwitch: true,
  available: true,
  status: 'ready',
};

const choice = (over: Partial<IModelChoice> = {}): IModelChoice => ({
  provider: 'claude',
  model: 'opus',
  effort: 'medium',
  ultracode: false,
  ...over,
});

const open = (
  value: IModelChoice,
  onChange = vi.fn(),
  offersUltracode?: boolean,
  ultracodeAsked?: boolean | null,
) => {
  const view = render(
    <ModelPicker
      value={value}
      agents={[agent]}
      locked
      onChange={onChange}
      offersUltracode={offersUltracode}
      ultracodeAsked={ultracodeAsked}
    />,
  );
  const pill = view.container.querySelector<HTMLButtonElement>('.hx-model');
  if (!pill) throw new Error('no pill');
  act(() => pill.click());
  return { view, pill, onChange };
};

describe('ultracode in the picker', () => {
  it('is a switch of its own under the slider on a model that runs it, and turns on keeping the depth', () => {
    const { view, onChange } = open(choice());
    const toggle = view.getByRole('switch', { name: 'Ultracode' });
    expect(toggle.getAttribute('aria-checked')).toBe('false');
    expect(view.container.querySelector('.hx-picker__ultra-hint')?.textContent).toBe(
      'Subagents on every task',
    );
    expect(view.container.querySelector('.hx-picker__ultra')?.getAttribute('title')).toBe(
      'Dynamic workflows with subagents on every task',
    );
    act(() => toggle.click());
    expect(onChange).toHaveBeenCalledWith(choice({ ultracode: true }));
  });

  it('is not offered on a model without it, nor where the start does not take the mode', () => {
    expect(open(choice({ model: 'haiku', effort: null })).view.queryByRole('switch')).toBeNull();
    cleanup();
    const { view, pill } = open(choice({ ultracode: true }), vi.fn(), false);
    expect(view.queryByRole('switch')).toBeNull();
    expect(pill.dataset.ultracode).toBeUndefined();
  });

  it('marks the pill apart from «model · depth», which reads as before', () => {
    const { pill } = open(choice({ ultracode: true }));
    expect(pill.dataset.ultracode).toBe('true');
    expect(pill.querySelector('.hx-model__ultra')?.getAttribute('aria-label')).toBe('Ultracode');
    expect(pill.querySelector('.hx-model__name')?.textContent).toBe('Opus 5.5');
    expect(pill.querySelector('.hx-model__effort')?.textContent).toBe('Medium');
    cleanup();
    expect(open(choice()).pill.dataset.ultracode).toBeUndefined();
  });

  it('names every stop under the slider and marks the chosen one', () => {
    const { view } = open(choice());
    const labels = Array.from(view.container.querySelectorAll<HTMLElement>('.hx-effort__label'));
    expect(labels.map((label) => label.textContent)).toEqual([
      'Low',
      'Medium',
      'High',
      'Very high',
      'Maximum',
    ]);
    expect(labels.map((label) => label.dataset.chosen)).toEqual([
      undefined,
      'true',
      undefined,
      undefined,
      undefined,
    ]);
  });

  it('leaves the depth a slider of the model’s stops, whatever the mode', () => {
    const { view, onChange } = open(choice({ ultracode: true }));
    const slider = view.getByRole('slider');
    const stops = slider.querySelectorAll<HTMLElement>('.hx-effort__stop');
    expect(stops).toHaveLength(levels.length);
    expect(stops[0].style.getPropertyValue('--k')).toBe('0');
    expect(stops[2].style.getPropertyValue('--k')).toBe('0.5');
    expect(stops[4].style.getPropertyValue('--k')).toBe('1');
    expect(slider.style.getPropertyValue('--p')).toBe('0.25');
    fireEvent.keyDown(window, { key: 'ArrowRight' });
    expect(onChange).toHaveBeenCalledWith(choice({ effort: 'high', ultracode: true }));
  });

  it('shows on the pill the mode the agent was seen in, and an ask that waits only on the switch, said as waiting', () => {
    // Seen off, asked on: the pill stays plain, the switch is on and says it waits for the agent.
    const { view, pill } = open(choice(), vi.fn(), true, true);
    expect(pill.dataset.ultracode).toBeUndefined();
    expect(pill.querySelector('.hx-model__ultra')).toBeNull();
    const row = view.container.querySelector<HTMLElement>('.hx-picker__ultra');
    expect(row?.dataset.on).toBeUndefined();
    expect(row?.dataset.pending).toBe('true');
    expect(view.getByRole('switch', { name: 'Ultracode' }).getAttribute('aria-checked')).toBe('true');
    expect(row?.querySelector('.hx-picker__ultra-hint')?.textContent).toBe(
      'Turns on once the agent confirms',
    );
    cleanup();
    // Seen on, asked off: the pill still glows until the agent says it is off.
    const off = open(choice({ ultracode: true }), vi.fn(), true, false);
    expect(off.pill.dataset.ultracode).toBe('true');
    expect(off.view.getByRole('switch', { name: 'Ultracode' }).getAttribute('aria-checked')).toBe('false');
    expect(off.view.container.querySelector('.hx-picker__ultra-hint')?.textContent).toBe(
      'Turns off once the agent confirms',
    );
  });

  it('says nothing waits once the agent is seen in the mode asked', () => {
    const { view, pill } = open(choice({ ultracode: true }), vi.fn(), true, true);
    expect(pill.dataset.ultracode).toBe('true');
    const row = view.container.querySelector<HTMLElement>('.hx-picker__ultra');
    expect(row?.dataset.pending).toBeUndefined();
    expect(row?.querySelector('.hx-picker__ultra-hint')?.textContent).toBe('Subagents on every task');
  });

  it('keeps the mode on to a model that runs it and drops it on one that does not', () => {
    const on = choice({ ultracode: true });
    expect(withModel(on, agent, sonnet).ultracode).toBe(true);
    expect(withModel(on, agent, haiku)).toEqual({
      provider: 'claude',
      model: 'haiku',
      effort: null,
      ultracode: false,
    });
  });
});

describe('several models for one prompt', () => {
  const openMulti = (chosen: IModelChoice[]) => {
    const onChange = vi.fn();
    const onToggle = vi.fn();
    const view = render(
      <ModelPicker
        value={chosen[0]}
        agents={[agent]}
        locked={false}
        onChange={onChange}
        multi={{ chosen, onToggle }}
      />,
    );
    const pill = view.container.querySelector<HTMLButtonElement>('.hx-model');
    if (!pill) throw new Error('no pill');
    act(() => pill.click());
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-picker__center')?.click());
    return { view, pill, onChange, onToggle };
  };

  it('shows how many more the prompt starts on, and marks the chosen models', () => {
    const { view, pill } = openMulti([choice(), choice({ model: 'sonnet' })]);
    expect(pill.querySelector('.hx-model__more')?.textContent).toBe('+1');
    const boxes = view.getAllByRole('checkbox');
    expect(boxes.map((box) => box.getAttribute('aria-checked'))).toEqual(['true', 'true', 'false']);
  });

  it('adds a model with the box or with Shift, and a plain click is the one model', () => {
    const { view, onChange, onToggle } = openMulti([choice()]);
    fireEvent.click(view.getByRole('checkbox', { name: 'Also run on Sonnet 5.5' }));
    expect(onToggle).toHaveBeenLastCalledWith(withModel(choice(), agent, sonnet));
    fireEvent.click(view.getByRole('option', { name: /Haiku 4.5/ }), { shiftKey: true });
    expect(onToggle).toHaveBeenLastCalledWith(withModel(choice(), agent, haiku));
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(view.getByRole('option', { name: /Sonnet 5.5/ }));
    expect(onChange).toHaveBeenCalledWith(withModel(choice(), agent, sonnet));
  });
});

describe('a handoff from the picker', () => {
  const gpt: AgentModel = {
    id: 'gpt-5.5',
    label: 'GPT-5.5',
    hint: '',
    efforts: levels,
    defaultEffort: 'high',
  };
  const codex: IPickerAgent = { ...agent, provider: 'codex', name: 'Codex', short: 'Codex', models: [gpt] };

  it('offers the other agent’s models as a handoff, and the chat’s own as the switch they were', () => {
    const onChange = vi.fn();
    const onPick = vi.fn();
    const view = render(
      <ModelPicker
        value={choice()}
        agents={[agent]}
        locked
        onChange={onChange}
        handoff={{ agents: [codex], onPick }}
      />,
    );
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-model')?.click());
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-picker__center')?.click());
    const segments = [...view.container.querySelectorAll<HTMLButtonElement>('.hx-seg__item')];
    expect(segments.map((s) => s.textContent)).toEqual(['Claude', 'Codex']);
    const handoff = view.container.querySelector<HTMLButtonElement>('.hx-option[data-handoff]');
    expect(handoff?.textContent).toContain('Continue in Codex');
    act(() => handoff?.click());
    expect(onPick).toHaveBeenCalledWith(codex, gpt);
    expect(onChange).not.toHaveBeenCalled();
  });

  it('shows only the chat’s own agent without one', () => {
    const view = render(<ModelPicker value={choice()} agents={[agent]} locked onChange={vi.fn()} />);
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-model')?.click());
    act(() => view.container.querySelector<HTMLButtonElement>('.hx-picker__center')?.click());
    expect(view.container.querySelector('.hx-seg')).toBeNull();
    expect(view.container.querySelector('.hx-option[data-handoff]')).toBeNull();
  });
});
