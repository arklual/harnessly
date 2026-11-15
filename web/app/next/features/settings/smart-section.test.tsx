// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { latestByKey, litChanges, LIT_MS } from '@/next/bindings/settings-changes';
import { ChangesGroup } from '@/next/features/settings/changed-mark';
import { readSettingChanges, type ISettingChange } from '@/next/features/settings/changes-api';
import { isRefused } from '@/next/features/settings/platform-answer';
import ProposalCard from '@/next/features/settings/proposal-card';
import { readSmartView, type ISmartView } from '@/next/features/settings/smart-api';
import SmartSection from '@/next/features/settings/smart-section';
import { pinLocale } from '@/next/i18n';

beforeAll(() => {
  pinLocale('en');
  // A wide window: the menu stays by its button (on a phone it is a sheet in the page's body).
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const NOW = 1_800_000_000_000;

// GET <platform>/background-tasks as the runtime answers it (cmd/runtime/routes_background.go).
const tasks = {
  features: [
    {
      id: 'notify-summary',
      title: 'Notification summaries',
      description: 'One short line of what the agent did.',
      live: true,
      provider: 'deepseek',
      model: 'deepseek-chat',
      last: { at: NOW, ok: false, reason: 'unauthorized', message: 'The key is not accepted.' },
    },
    {
      id: 'next-step',
      title: 'Next-step hints',
      description: 'What to ask the agent next.',
      live: false,
      provider: null,
      model: null,
    },
  ],
  providers: [{ id: 'deepseek', name: 'DeepSeek', api: 'openai', models: [{ id: 'deepseek-chat' }] }],
};

// GET <platform>/settings-changes as the runtime answers it (cmd/runtime/routes_settingsctl.go).
const journal = {
  changes: [
    {
      id: 'sc_2',
      at: NOW - 60_000,
      key: 'background.next-step',
      title: 'Next-step hints',
      old: 'default',
      new: 'deepseek/deepseek-chat',
      oldLabel: 'as now',
      newLabel: 'DeepSeek · deepseek-chat',
      by: { kind: 'agent', conv: 'c1', title: 'Settings' },
      via: 'settings-chat',
      why: 'The person asked for a cheaper model',
    },
    {
      id: 'sc_1',
      at: NOW - 120_000,
      key: 'runtime.idleStop',
      title: 'Stop an idle chat',
      old: '1h',
      new: '2h',
      oldLabel: 'after 1 hour',
      newLabel: 'after 2 hours',
      by: { kind: 'person' },
      via: 'page',
    },
  ],
};

const view = (): ISmartView => {
  const read = readSmartView(tasks);
  if (isRefused(read)) throw new Error('refused');
  return read;
};
const changes = (): ISettingChange[] => {
  const read = readSettingChanges(journal);
  if (isRefused(read)) throw new Error('refused');
  return read;
};

describe('the answer of the background tasks', () => {
  it('reads «as now» as no choice and a failed last call as its words', () => {
    const read = view();
    expect(read.features[0]?.choice).toEqual({ provider: 'deepseek', model: 'deepseek-chat' });
    expect(read.features[0]?.failure).toBe('The key is not accepted.');
    expect(read.features[1]?.choice).toBeNull();
    expect(read.features[1]?.failure).toBeNull();
  });
});

describe('the rows to light', () => {
  it('are the settings whose newest change is a recent one of an agent', () => {
    const list = changes();
    expect(litChanges(list, NOW).map((change) => change.key)).toEqual(['background.next-step']);
    expect(litChanges(list, NOW + LIT_MS)).toEqual([]);
  });

  it('go dark when the person changes the setting after the agent', () => {
    const [agent] = changes();
    if (!agent) throw new Error('no change');
    const back: ISettingChange = { ...agent, id: 'sc_3', at: NOW, by: 'person', chat: '' };
    expect(latestByKey([back, agent]).get(agent.key)?.id).toBe('sc_3');
    expect(litChanges([back, agent], NOW)).toEqual([]);
  });
});

describe('the smart features', () => {
  const draw = (onPick = vi.fn(), onUndo = vi.fn()) => {
    render(
      <SmartSection
        state={{ k: 'ready', view: view() }}
        busy={new Set()}
        agentModels={{ claude: [], codex: [] }}
        marks={{ 'background.next-step': { was: 'as now', busy: false, onUndo } }}
        onPick={onPick}
        onOpenProviders={() => {}}
      />,
    );
    return { onPick, onUndo };
  };

  it('says why the last call failed, and that a feature not live yet keeps the choice', () => {
    draw();
    expect(screen.getByText('It failed last time: The key is not accepted.')).toBeTruthy();
    expect(screen.getByText(/works only “as now”/)).toBeTruthy();
    expect(screen.getByRole('button', { name: /DeepSeek · deepseek-chat/ })).toBeTruthy();
  });

  it('lights the row an agent changed with what it was and a way back', () => {
    const { onUndo } = draw();
    const mark = screen.getByText(/Changed by the agent/);
    expect(mark.textContent).toContain('as now');
    expect(mark.closest('.hx-set__row')?.getAttribute('data-changed')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Undo' }));
    expect(onUndo).toHaveBeenCalled();
  });

  it('gives a feature to a provider’s model, and back to the chat’s agent', () => {
    const { onPick } = draw();
    fireEvent.click(screen.getByRole('button', { name: /As now/ }));
    fireEvent.click(screen.getByRole('option', { name: /DeepSeek · deepseek-chat/ }));
    expect(onPick).toHaveBeenLastCalledWith('next-step', { provider: 'deepseek', model: 'deepseek-chat' });

    fireEvent.click(screen.getByRole('button', { name: /DeepSeek · deepseek-chat/ }));
    fireEvent.click(screen.getAllByRole('option', { name: /As now/ })[0] as HTMLElement);
    expect(onPick).toHaveBeenLastCalledWith('notify-summary', null);
  });

  it('shows loading and a failure apart from the list', () => {
    const { rerender, container } = render(
      <SmartSection
        state={{ k: 'loading' }}
        busy={new Set()}
        agentModels={{ claude: [], codex: [] }}
        marks={{}}
        onPick={() => {}}
        onOpenProviders={() => {}}
      />,
    );
    expect(container.querySelector('.hx-sk')).toBeTruthy();
    rerender(
      <SmartSection
        state={{ k: 'failed', text: 'No connection', onRetry: () => {} }}
        busy={new Set()}
        agentModels={{ claude: [], codex: [] }}
        marks={{}}
        onPick={() => {}}
        onOpenProviders={() => {}}
      />,
    );
    expect(screen.getByRole('alert').textContent).toContain('No connection');
  });
});

describe('the journal', () => {
  it('says who changed what, from what to what, and the agent’s why', () => {
    render(<ChangesGroup journal={{ k: 'ready', changes: changes() }} now={NOW} />);
    expect(screen.getByText('Agent of “Settings”')).toBeTruthy();
    expect(screen.getByText('You')).toBeTruthy();
    expect(screen.getByText('The person asked for a cheaper model')).toBeTruthy();
    expect(screen.getByText('after 2 hours')).toBeTruthy();
  });

  it('shows reading, nothing changed and a failure as three different things', () => {
    const { rerender, container } = render(
      <ChangesGroup journal={{ k: 'ready', changes: null }} now={NOW} />,
    );
    expect(container.querySelector('.hx-sk')).toBeTruthy();
    rerender(<ChangesGroup journal={{ k: 'ready', changes: [] }} now={NOW} />);
    expect(screen.getByText('Nobody has changed the settings yet')).toBeTruthy();
    rerender(<ChangesGroup journal={{ k: 'failed', text: 'No connection', onRetry: () => {} }} now={NOW} />);
    expect(screen.getByRole('alert').textContent).toContain('No connection');
  });
});

describe('the card of a proposal', () => {
  const proposal = {
    id: 'sp_1',
    key: 'runtime.idleStop',
    title: 'Stop an idle chat',
    oldLabel: 'after 1 hour',
    newLabel: 'never',
    why: 'Long builds are stopped halfway',
    conv: 'c7',
  };

  it('shows what changes and sends the person’s yes', async () => {
    const onAnswer = vi.fn(async () => null);
    render(<ProposalCard proposal={proposal} closing={false} onAnswer={onAnswer} />);
    expect(screen.getByText('Stop an idle chat')).toBeTruthy();
    expect(screen.getByText('never')).toBeTruthy();
    expect(screen.getByText('Long builds are stopped halfway')).toBeTruthy();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Yes, change it' }));
    });
    expect(onAnswer).toHaveBeenCalledWith(proposal, 'approve');
  });

  it('sends the no, and says why an answer was not taken', async () => {
    const onAnswer = vi.fn(async () => 'The proposal is closed already.');
    render(<ProposalCard proposal={proposal} closing={false} onAnswer={onAnswer} />);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'No' }));
    });
    expect(onAnswer).toHaveBeenCalledWith(proposal, 'decline');
    expect(screen.getByRole('alert').textContent).toBe('The proposal is closed already.');
  });
});
