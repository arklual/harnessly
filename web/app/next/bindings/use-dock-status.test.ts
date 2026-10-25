import { describe, expect, it } from 'vitest';
import { countWaiting, pendingSentence } from '@/next/bindings/use-dock-status';
import type { IPickerAgent } from '@/next/features/composer/agents';
import { WHEN } from '@/next/features/composer/launch-chain';
import type { MessageView } from '@/next/stores/messages';

const agent: IPickerAgent = {
  provider: 'claude',
  name: 'Claude Code',
  short: 'Claude',
  models: [
    { id: 'sonnet', label: 'Sonnet 5.5', hint: '', efforts: ['low', 'high'], defaultEffort: 'high' },
    { id: 'opus', label: 'Opus 5.5', hint: '', efforts: ['low', 'high'], defaultEffort: 'high' },
  ],
  canSwitch: true,
  available: true,
  status: 'ready',
};

describe('the sentence for a change that waits', () => {
  it('names the model and its effort, and when it takes effect', () => {
    expect(
      pendingSentence({ when: WHEN.turn, model: 'sonnet', effort: 'high', ultracode: null }, agent),
    ).toBe('Model will change to Sonnet 5.5 · effort: High after the turn');
    expect(pendingSentence({ when: WHEN.launch, model: 'opus', effort: null, ultracode: null }, agent)).toBe(
      'Model will change to Opus 5.5 when the agent starts',
    );
  });

  it('says only the effort when the model stays', () => {
    expect(pendingSentence({ when: WHEN.turn, model: null, effort: 'low', ultracode: null }, agent)).toBe(
      'Effort will change to Low after the turn',
    );
    expect(pendingSentence({ when: WHEN.launch, model: null, effort: 'xhigh', ultracode: null }, agent)).toBe(
      'Effort will change to Very high when the agent starts',
    );
  });

  it('says ultracode on its own when only the mode changes, and beside the model when both do', () => {
    expect(pendingSentence({ when: WHEN.turn, model: null, effort: null, ultracode: true }, agent)).toBe(
      'Ultracode turns on after the turn',
    );
    expect(pendingSentence({ when: WHEN.launch, model: null, effort: null, ultracode: false }, agent)).toBe(
      'Ultracode turns off when the agent starts',
    );
    expect(pendingSentence({ when: WHEN.turn, model: 'opus', effort: null, ultracode: true }, agent)).toBe(
      'Model will change to Opus 5.5 · ultracode on after the turn',
    );
  });
});

describe('messages the agent has not taken', () => {
  const message = (k: string) => ({ state: { k } }) as unknown as MessageView;
  it('counts the queued, the typed and the ones still going to the runtime', () => {
    expect(
      countWaiting(['queued', 'typing', 'in-input', 'submitting', 'accepted', 'failed'].map(message)),
    ).toBe(4);
  });
});
