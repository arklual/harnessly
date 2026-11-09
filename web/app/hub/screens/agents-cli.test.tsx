// @vitest-environment jsdom
// The machine's other CLIs (GitHub issue 58): the agent reports them, the page lists them under «Входы в CLI» and signs
// one in only in the console, by the id the machine gave, without showing a command of its own.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { Live } from '../lib/hooks';
import { t } from '../lib/i18n';
import { initialLive } from '../lib/live';
import type { CLIStatus, Machine } from '../lib/types';
import { MachineAgents } from './agents';

const m = vi.hoisted(() => ({
  api: {
    agents: { list: vi.fn(), login: vi.fn(), loginCancel: vi.fn() },
    machines: { open: vi.fn() },
  },
  openBlank: vi.fn(),
}));

vi.mock('../lib/env', () => ({
  useApi: () => m.api,
  useEnv: () => ({ api: m.api, openBlank: m.openBlank, origin: () => 'https://hub.example.com' }),
}));

const cli = (login: CLIStatus['login'], installed = true): CLIStatus => ({
  provider: 'example-cli',
  kind: 'cli',
  label: 'Example CLI',
  installed,
  login,
});

const machine = (clis?: CLIStatus[]): Machine =>
  ({
    id: 'm1',
    name: 'dev',
    status: 'online',
    agents: [],
    clis,
    policy: null,
    proxy: { all: null, claude: null, codex: null },
  }) as unknown as Machine;

const live = (mc: Machine): Live =>
  ({ state: { ...initialLive, machines: [mc] }, push: vi.fn() }) as unknown as Live;

beforeEach(() => {
  m.api.agents.list.mockResolvedValue({ agents: [] });
  m.api.agents.login.mockResolvedValue({
    login: { id: 'l1', provider: 'example-cli', method: 'console', state: 'starting' },
  });
  m.openBlank.mockReturnValue({ go: vi.fn(), close: vi.fn() });
});
afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe('CLI sign-ins', () => {
  it('lists none when the machine reports none', async () => {
    const mc = machine();
    render(<MachineAgents machine={mc} live={live(mc)} />);
    await act(async () => Promise.resolve());
    expect(screen.queryByText(t('clis.title'))).toBeNull();
  });

  it('signs a CLI in by console with its id', async () => {
    const mc = machine([cli('logged_out')]);
    render(<MachineAgents machine={mc} live={live(mc)} />);
    await act(async () => Promise.resolve());
    expect(screen.getByText(t('clis.title'))).toBeTruthy();
    expect(screen.getByText('Example CLI')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: t('agents.login') }));
    expect(screen.queryByText(/example-cli login/)).toBeNull();
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: t('login.console.button') }));
    });
    expect(m.api.agents.login).toHaveBeenCalledWith('m1', 'example-cli', 'console');
  });

  it('offers no sign-in to a CLI that is signed in or not installed', async () => {
    const mc = machine([
      cli('logged_in'),
      { ...cli('logged_out', false), provider: 'other-cli', label: 'Other' },
    ]);
    render(<MachineAgents machine={mc} live={live(mc)} />);
    await act(async () => Promise.resolve());
    expect(screen.getByText(t('clis.missing'))).toBeTruthy();
    expect(screen.queryByRole('button', { name: t('agents.login') })).toBeNull();
  });
});
