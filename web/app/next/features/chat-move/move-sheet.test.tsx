// @vitest-environment jsdom
import { createRef, type ReactNode } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import MoveSheet from '@/next/features/chat-move/move-sheet';
import type { IMoveTarget, IMoveTargets } from '@/next/features/chat-move/api';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const agents = [
  {
    provider: 'claude' as const,
    profile: '',
    short: 'Claude',
    models: [
      { id: 'opus', label: 'Opus', default: true },
      { id: 'sonnet', label: 'Sonnet', default: false },
    ],
  },
];
const target = (over: Partial<IMoveTarget> = {}): IMoveTarget => ({
  machineId: 'vm2',
  name: 'Roma VM',
  online: true,
  ok: true,
  checks: [{ id: 'project', ok: true }],
  filesOk: true,
  agents,
  ...over,
});
const data = (over: Partial<IMoveTargets> = {}): IMoveTargets => ({
  source: { ok: true, project: true, dirty: false, busy: false },
  targets: [target()],
  ...over,
});
const open = (
  load: () => Promise<IMoveTargets>,
  onMove = vi.fn().mockResolvedValue(undefined),
  onClose = vi.fn(),
) => {
  inRoot(<MoveSheet open onClose={onClose} load={load} onMove={onMove} />);
  return { onMove, onClose };
};
const send = () => screen.getByRole('button', { name: 'Continue' });

describe('continue on another machine', () => {
  it('moves to the one machine that is ready, with its default model, in one press', async () => {
    const { onMove, onClose } = open(async () => data());
    await screen.findByRole('radio', { name: /Roma VM/ });
    await waitFor(() => expect((send() as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(send());
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onMove).toHaveBeenCalledTimes(1);
    const choice = onMove.mock.calls[0]?.[0];
    expect(choice).toMatchObject({
      target: { machineId: 'vm2' },
      model: { id: 'opus' },
      withoutFiles: false,
    });
    expect(String(choice.opId).length).toBeGreaterThan(8);
  });

  it('keeps a machine that is down in the list, disabled, with the reason', async () => {
    open(async () =>
      data({
        targets: [
          target({
            machineId: 'vm3',
            name: 'Off VM',
            online: false,
            ok: false,
            code: 'offline',
            checks: [],
            agents: [],
          }),
          target(),
        ],
      }),
    );
    const off = await screen.findByRole('radio', { name: /Off VM/ });
    expect((off as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText('Offline')).toBeTruthy();
  });

  it('asks for «without local files» when the machine lacks the commit, and sends it', async () => {
    const { onMove } = open(async () =>
      data({
        targets: [
          target({
            filesOk: false,
            checks: [
              { id: 'project', ok: true },
              { id: 'commit', ok: false, code: 'commit_missing' },
            ],
          }),
        ],
      }),
    );
    await screen.findByRole('switch', { name: /without local files/i });
    await waitFor(() => expect((send() as HTMLButtonElement).disabled).toBe(true));
    fireEvent.click(screen.getByRole('switch', { name: /without local files/i }));
    await waitFor(() => expect((send() as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(send());
    await waitFor(() => expect(onMove).toHaveBeenCalled());
    expect(onMove.mock.calls[0]?.[0]).toMatchObject({ withoutFiles: true });
  });

  it('refuses uncommitted changes of the source in words and moves only without files', async () => {
    open(async () => data({ source: { ok: true, project: true, dirty: true, busy: false } }));
    expect(await screen.findByText(/uncommitted changes/)).toBeTruthy();
    expect((send() as HTMLButtonElement).disabled).toBe(true);
  });

  it('does not offer to move a chat whose agent still works', async () => {
    open(async () => data({ source: { ok: true, project: true, dirty: false, busy: true } }));
    expect(await screen.findByText(/still working/)).toBeTruthy();
    expect((send() as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows why a move failed, stays open, and keeps the same operation for the retry', async () => {
    const onMove = vi
      .fn()
      .mockRejectedValueOnce(new Error('The commit is missing'))
      .mockResolvedValue(undefined);
    const { onClose } = open(async () => data(), onMove);
    await screen.findByRole('radio', { name: /Roma VM/ });
    await waitFor(() => expect((send() as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(send());
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.click(send());
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onMove.mock.calls[1]?.[0].opId).toBe(onMove.mock.calls[0]?.[0].opId);
  });

  it('a second press while the move runs is not a second move', async () => {
    let finish: () => void = () => undefined;
    const onMove = vi.fn(() => new Promise<void>((resolve) => (finish = resolve)));
    open(async () => data(), onMove);
    await screen.findByRole('radio', { name: /Roma VM/ });
    await waitFor(() => expect((send() as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(send());
    fireEvent.click(send());
    expect(onMove).toHaveBeenCalledTimes(1);
    finish();
  });

  it('says so when no other machine is connected', async () => {
    open(async () => data({ targets: [] }));
    expect(await screen.findByText('No other machines are connected.')).toBeTruthy();
  });

  it('lets the person retry when the machines could not be asked', async () => {
    const load = vi.fn().mockRejectedValueOnce(new Error('No connection')).mockResolvedValue(data());
    open(load);
    expect(await screen.findByText('No connection')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /retry/i }));
    await screen.findByRole('radio', { name: /Roma VM/ });
  });
});
