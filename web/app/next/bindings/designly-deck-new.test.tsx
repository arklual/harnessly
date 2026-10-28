// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MachineAgent, MachineId } from '@/next/protocol/v2';
import type { IDeckCreateInput } from '@/next/features/designly/deck/deck-create';

const created = vi.fn();
let agents: MachineAgent[] = [];
let settingsOptions: { key: string; value: string }[] = [];

vi.mock('@/next/stores/machines', () => ({ useMachine: () => ({ machine: { agents } }) }));
vi.mock('@/next/bindings/settings-data', () => ({
  useAgentSettings: (_m: string, provider: string, enabled: boolean) => ({
    state:
      enabled && provider === 'claude'
        ? { k: 'ready', value: { options: settingsOptions } }
        : { k: 'loading' },
  }),
}));
vi.mock('@/next/features/designly/api', () => ({
  createDesign: (_m: string, body: unknown) => {
    created(body);
    return Promise.resolve({ id: 'p1', conv: 'c1' });
  },
  fetchSystems: () => Promise.resolve([]),
}));
vi.mock('@/next/bindings/design-conv', () => ({ bindDesignConv: () => undefined }));
vi.mock('@/next/routes/routes', () => ({
  goTo: () => undefined,
  ROUTE: { site: 'site', designly: 'designly' },
}));
vi.mock('@/next/ui/toast', () => ({ toast: () => undefined }));
vi.mock('@/next/features/designly/deck/deck-create', () => ({
  default: (props: { onCreate: (input: IDeckCreateInput) => Promise<void> }) => (
    <button
      onClick={() =>
        void props.onCreate({
          title: 'T',
          brief: 'B',
          deck: { minutes: 0, slides: null, tone: 'talk', script: true, style: null },
          designSystem: 'acme',
          attachmentIds: [],
        })
      }
    >
      create
    </button>
  ),
}));

import DeckCreateView from '@/next/bindings/designly-deck-new';

const model = (id: string) => ({
  id,
  label: id,
  hint: '',
  efforts: ['low', 'medium', 'high'] as const,
  defaultEffort: 'high' as const,
});

describe('the create screen of a presentation', () => {
  beforeEach(() => {
    localStorage.clear();
    created.mockClear();
    agents = [
      {
        provider: 'claude',
        version: '1',
        status: 'ready',
        models: [model('fable'), model('opus'), model('sonnet')].map((m) => ({
          ...m,
          efforts: [...m.efforts],
        })),
        canSwitchModel: true,
      },
    ];
  });
  afterEach(cleanup);

  const press = async () => {
    render(<DeckCreateView machine={'m1' as MachineId} />);
    await act(async () => {
      screen.getByText('create').click();
    });
  };

  it('starts the deck on the model and effort of the CLI settings, not on the first model', async () => {
    settingsOptions = [
      { key: 'model', value: 'opus' },
      { key: 'effort', value: 'medium' },
    ];
    await press();
    expect(created).toHaveBeenCalledWith(
      expect.objectContaining({ provider: 'claude', model: 'opus', effort: 'medium', designSystem: 'acme' }),
    );
  });

  it('takes the first model when nothing else is known, and remembers what it started on', async () => {
    settingsOptions = [];
    await press();
    expect(created).toHaveBeenCalledWith(expect.objectContaining({ model: 'fable' }));
    expect(localStorage.getItem('hx-next-new-chat-choice')).toContain('fable');
  });
});
