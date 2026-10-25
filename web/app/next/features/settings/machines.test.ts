import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { isMain, machineRows } from '@/next/features/settings/machines';
import type { Machine } from '@/next/protocol/v2';
import type { MachineView } from '@/next/stores/machines';

beforeAll(() => pinLocale('en'));

const NOW = 1_792_656_000_000;

const view = (patch: Partial<Machine>): MachineView => ({
  machine: {
    id: 'm1',
    name: 'Runner',
    host: 'runner.example.com',
    reach: 'online',
    lastSeenAt: NOW,
    runtime: null,
    agents: [
      { provider: 'claude', version: '2.1.0', status: 'ready', models: [], canSwitchModel: false },
      { provider: 'codex', version: null, status: 'missing', models: [], canSwitchModel: false },
    ],
    ...patch,
  },
  runtime: 'v2',
  epoch: null,
  features: [],
  limits: null,
});

describe('isMain', () => {
  it('is the machine the person chose, or the only one', () => {
    expect(isMain('a', 'a', 2)).toBe(true);
    expect(isMain('b', 'a', 2)).toBe(false);
    expect(isMain('a', null, 1)).toBe(true);
    expect(isMain('a', null, 2)).toBe(false);
  });
});

describe('machineRows', () => {
  it('tells the agents with versions, and what is missing', () => {
    const [row] = machineRows([view({})], null, NOW);
    expect(row).toMatchObject({
      id: 'm1',
      host: 'runner.example.com',
      online: true,
      state: 'online',
      agents: 'Claude Code 2.1.0 · Codex is not installed',
      main: true,
      canMain: false,
    });
  });

  it('tells an agent that waits for a sign-in, and what to run', () => {
    const [row] = machineRows(
      [
        view({
          agents: [
            { provider: 'claude', version: '2.1.0', status: 'ready', models: [], canSwitchModel: false },
            {
              provider: 'codex',
              version: '0.152.0',
              status: 'login-required',
              models: [],
              canSwitchModel: false,
            },
          ],
        }),
      ],
      null,
      NOW,
    );
    expect(row?.agents).toBe('Claude Code 2.1.0 · Codex: sign-in needed (run codex login)');
  });

  it('lets only a machine that answers become the main one', () => {
    const rows = machineRows(
      [view({ id: 'a' }), view({ id: 'b' }), view({ id: 'c', reach: 'unreachable', lastSeenAt: null })],
      'a',
      NOW,
    );
    expect(rows.map((row) => [row.main, row.canMain])).toEqual([
      [true, false],
      [false, true],
      [false, false],
    ]);
    expect(rows[2]?.state).toBe('not answering');
  });

  it('says since when a machine does not answer', () => {
    const [row] = machineRows([view({ reach: 'unreachable', lastSeenAt: NOW - 5 * 60_000 })], 'm1', NOW);
    expect(row?.state).toContain('not answering, was online');
  });
});
