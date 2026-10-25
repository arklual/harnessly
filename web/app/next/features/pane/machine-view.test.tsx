// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import MachineView from '@/next/features/pane/machine-view';
import type { MachineProc, MachineStats } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const GB = 1024 ** 3;
const NOW = 1_792_656_000_000;

const proc = (patch: Partial<MachineProc>): MachineProc => ({
  pid: 10,
  startTime: '77',
  name: 'node · vite',
  command: 'node vite',
  cpu: 22,
  rss: GB,
  kind: 'dev',
  port: null,
  stuck: false,
  guard: false,
  ...patch,
});

const stats = (procs: MachineProc[]): MachineStats => ({
  at: NOW,
  cpu: 0.4,
  cores: [0.4, 0.2],
  mem: { used: 4 * GB, total: 8 * GB },
  net: { rx: 1000, tx: 500 },
  disk: {
    path: '/home',
    total: 100 * GB,
    free: 50 * GB,
    parts: [
      { id: 'other', bytes: 50 * GB },
      { id: 'free', bytes: 50 * GB },
    ],
    measuredAt: null,
    partial: false,
  },
  history: [],
  procs,
});

const props = {
  name: 'dev-1',
  error: null,
  onRetry: () => undefined,
  pending: new Map(),
  hidden: new Set<string>(),
  onStop: () => undefined,
};

describe('MachineView', () => {
  it('stops what the user may and marks what the machine needs as protected', () => {
    const onStop = vi.fn();
    const vite = proc({});
    const dockerd = proc({ pid: 11, startTime: '78', name: 'dockerd', guard: true });
    render(<MachineView {...props} stats={stats([vite, dockerd])} onStop={onStop} />);
    expect(screen.getAllByRole('button', { name: /^Stop / })).toHaveLength(1);
    expect(screen.getByText('protected')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Stop node · vite' }));
    expect(onStop).toHaveBeenCalledWith(vite);
  });

  it('shows a stop that waits for the machine, and leaves out a process that was stopped', () => {
    const vite = proc({});
    const gone = proc({ pid: 12, startTime: '79', name: 'java · gradle' });
    render(
      <MachineView
        {...props}
        stats={stats([vite, gone])}
        pending={new Map([['10:77', 'busy' as const]])}
        hidden={new Set(['12:79'])}
      />,
    );
    const button = screen.getByRole('button', { name: 'Stop node · vite' });
    expect((button as HTMLButtonElement).disabled).toBe(true);
    expect(button.textContent).toBe('Stopping…');
    expect(screen.queryByText('java · gradle')).toBeNull();
  });

  it('says there is nothing heavy, and says the chart has no history yet', () => {
    render(<MachineView {...props} stats={stats([])} />);
    expect(screen.getByText('No heavy processes')).toBeTruthy();
    expect(screen.getAllByText('The chart appears in a couple of minutes')).toHaveLength(2);
  });

  it('offers a retry when nothing loaded, and keeps the figures through a later stumble', () => {
    const onRetry = vi.fn();
    const { rerender } = render(<MachineView {...props} stats={null} error="No network" onRetry={onRetry} />);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalled();
    rerender(<MachineView {...props} stats={stats([])} error="No network" onRetry={onRetry} />);
    expect(screen.getAllByText('Processor').length).toBeGreaterThan(0);
    expect(screen.getByText(/No network/)).toBeTruthy();
  });
});
