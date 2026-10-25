// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import ModelSwitchRow, { switchModels } from '@/next/features/chat/model-switch';
import type { ModelSwitchItem } from '@/next/features/chat/types';

afterEach(cleanup);

const names: Record<string, string> = { opus: 'Opus 5.5', sonnet: 'Sonnet 5.5' };
const label = (id: string) => names[id] ?? id;

const item = (over: Partial<ModelSwitchItem> = {}): ModelSwitchItem => ({
  kind: 'switch',
  key: 'switch:1',
  at: 1,
  from: { id: 'opus', effort: 'low' },
  to: { id: 'sonnet', effort: 'high' },
  ...over,
});

describe('a change of model in the feed', () => {
  it('reads «Model: Opus 5.5 → Sonnet 5.5 · effort: High»', () => {
    const view = render(<ModelSwitchRow item={item()} label={label} />);
    expect(view.container.textContent).toContain('Model');
    expect(view.container.textContent).toContain('Opus 5.5 → Sonnet 5.5');
    expect(view.container.textContent).toContain('effort: High');
  });

  it('names only the new model when none was known, and no effort when none was asked', () => {
    expect(switchModels(item({ from: null }), label)).toBe('Sonnet 5.5');
    const view = render(
      <ModelSwitchRow item={item({ from: null, to: { id: 'sonnet', effort: null } })} label={label} />,
    );
    expect(view.container.textContent).not.toContain('effort');
  });

  it('does not arrow a model into itself when only the effort changed', () => {
    expect(switchModels(item({ to: { id: 'opus', effort: 'high' } }), label)).toBe('Opus 5.5');
  });

  it('says ultracode turned on or off beside the model', () => {
    const on = render(
      <ModelSwitchRow item={item({ to: { id: 'sonnet', effort: 'high', ultracode: true } })} label={label} />,
    );
    expect(on.container.textContent).toContain('ultracode on');
    cleanup();
    const off = render(
      <ModelSwitchRow
        item={item({
          from: { id: 'sonnet', effort: 'high', ultracode: true },
          to: { id: 'sonnet', effort: 'high' },
        })}
        label={label}
      />,
    );
    expect(off.container.textContent).toContain('ultracode off');
    cleanup();
    expect(render(<ModelSwitchRow item={item()} label={label} />).container.textContent).not.toContain(
      'ultracode',
    );
  });

  it('says a change the agent did not take did not take, and why', () => {
    const kept = render(<ModelSwitchRow item={item({ refused: 'conflict' })} label={label} />);
    expect(kept.container.textContent).toBe(
      'The model did not change to Sonnet 5.5: the agent kept the one it had',
    );
    cleanup();
    const mode = render(<ModelSwitchRow item={item({ refused: 'option-unknown' })} label={label} />);
    expect(mode.container.textContent).toContain('this model cannot do that');
  });
});
