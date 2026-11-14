// @vitest-environment jsdom
// The plate's host with a machine in the red: its items are rebuilt on every render (a machine's record is a new object
// each time), and the list kept as state must still settle. It did not, and every page with a red machine fell with
// React's «Too many re-renders».
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';

const stats = (id: string) => ({
  at: 1,
  cpu: 0.97,
  cores: [0.97],
  mem: { used: 9, total: 10 },
  net: { rx: 0, tx: 0 },
  disk: { free: 10, total: 100 },
  history: [],
  procs: [
    {
      pid: 1,
      startTime: 's',
      name: `agent-${id}`,
      command: 'x',
      cpu: 90,
      rss: 5,
      kind: 'agent',
      port: null,
      stuck: false,
      guard: false,
    },
  ],
});
vi.mock('@/next/features/pane/machine/api', async (orig) => ({
  ...(await orig<object>()),
  loadMachineStats: vi.fn(async (id: string) => ({ ok: true, value: stats(id) })),
}));
import SmartNoticeHost from './smart-notice';
import { SNOOZE_MS, snoozeLevel, snoozeMachine } from '@/next/stores/machine-snooze';

beforeAll(() => {
  pinLocale('ru');
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  window.matchMedia ??= (() => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const red = (id: string, metric: 'cpu' | 'mem' | 'io' | 'disk') => ({
  id,
  name: id,
  since: Date.now() - 5000,
  urgent: metric !== 'disk',
  alert: { key: `${id}:1`, icon: 'cpu', text: metric, metric, waiting: metric === 'io' ? ['git'] : [] },
});

it.each([
  ['one machine', [red('m1', 'cpu')]],
  ['several machines', [red('m1', 'io'), red('m2', 'mem'), red('m3', 'disk')]],
])('settles with %s in the red', async (_, machines) => {
  const errors = vi.spyOn(console, 'error').mockImplementation(() => undefined);
  const props = {
    person: 'p',
    notices: [],
    machineNotices: machines as never,
    machine: 'm1' as never,
    currentId: null,
    onOpenChat: vi.fn(),
    onOpenCenter: vi.fn(),
    onOpenMachine: vi.fn(),
    onOpenSettings: vi.fn(),
  };
  const view = render(<SmartNoticeHost {...props} />);
  await act(async () => {
    await new Promise((done) => setTimeout(done, 50));
  });
  view.rerender(<SmartNoticeHost {...props} machineNotices={[...machines] as never} />);
  await act(async () => {
    await new Promise((done) => setTimeout(done, 50));
  });
  expect(errors.mock.calls.filter(([said]) => String(said).includes('smart notice failed'))).toEqual([]);
  expect(view.container.querySelector('.hx-sn')).not.toBeNull();
  errors.mockRestore();
});

const hostProps = (machines: unknown[]) => ({
  person: 'snooze-test',
  notices: [],
  machineNotices: machines as never,
  machine: 'm1' as never,
  currentId: null,
  onOpenChat: vi.fn(),
  onOpenCenter: vi.fn(),
  onOpenMachine: vi.fn(),
  onOpenSettings: vi.fn(),
});

const settle = (ms = 50) =>
  act(async () => {
    await new Promise((done) => setTimeout(done, ms));
  });

// a disk with gigabytes left is a warning for the bell, not a card over the chat's field.
it('keeps a machine that only warns off the plate', async () => {
  const warn = { ...red('m9', 'disk'), name: 'warn-machine', urgent: false };
  render(<SmartNoticeHost {...hostProps([warn])} />);
  await settle();
  expect(screen.queryByText('warn-machine')).toBeNull();
});

it('keeps a machine put away with «Не сейчас» off the plate until its level changes', async () => {
  const stop = { ...red('m8', 'io'), name: 'stalled-machine' };
  window.localStorage.clear();
  const view = render(<SmartNoticeHost {...hostProps([stop])} />);
  await settle();
  expect(screen.queryAllByText('stalled-machine').length).toBeGreaterThan(0);
  act(() => snoozeMachine('m8', snoozeLevel('io', true)));
  // The line leaves with its closing animation first.
  await settle(1500);
  expect(screen.queryByText('stalled-machine')).toBeNull();
  expect(window.localStorage.getItem('hx-machine-snooze:snooze-test')).toContain('io:stop');
  // Another cause is another level: it shows again.
  const disk = { ...stop, alert: { ...stop.alert, key: 'm8:2', metric: 'disk' } };
  view.rerender(<SmartNoticeHost {...hostProps([disk])} />);
  await settle();
  expect(screen.queryAllByText('stalled-machine').length).toBeGreaterThan(0);
  expect(SNOOZE_MS).toBeGreaterThanOrEqual(3 * 60 * 60_000);
});
