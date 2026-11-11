// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const state = vi.hoisted(() => ({
  api: {} as Record<string, unknown>,
  local: true,
  admin: true,
}));

vi.mock('../../context', () => ({ useOrg: () => ({ api: state.api }) }));
vi.mock('../../../lib/hooks', () => ({ useNow: () => 0 }));
vi.mock('../../../lib/session', () => ({
  useSession: () => ({
    config: { connect: { local: { available: state.local } }, profiles: { container: false } },
    me: { user: { isAdmin: state.admin } },
  }),
}));

import { AddMachineSheet } from './add';

const tenants = [{ id: 't1', slug: 't1', name: 'Основное' }];

afterEach(() => cleanup());

const open = (onAdded = vi.fn(), onClose = vi.fn()) =>
  render(<AddMachineSheet open tenants={tenants} onClose={onClose} onAdded={onAdded} />);

describe('Add machine: the machine next to the hub', () => {
  it('offers it and takes it into the tenant in one action', async () => {
    state.local = true;
    state.admin = true;
    state.api = {
      claimLocalMachine: vi.fn(async () => ({ id: 'm1', name: 'Эта машина', status: 'online' })),
    };
    const onAdded = vi.fn();
    open(onAdded);
    fireEvent.click(screen.getByRole('button', { name: /Подключить эту машину/ }));
    await waitFor(() => expect(onAdded).toHaveBeenCalled());
    expect(state.api.claimLocalMachine).toHaveBeenCalledWith('t1');
    expect(screen.getByText(/Машина Эта машина подключена/)).toBeTruthy();
  });

  it('does not offer it when no local machine is waiting, or to a person who is no instance admin', () => {
    state.api = {};
    state.local = false;
    state.admin = true;
    open();
    expect(screen.queryByRole('button', { name: /Подключить эту машину/ })).toBeNull();
    cleanup();
    state.local = true;
    state.admin = false;
    open();
    expect(screen.queryByRole('button', { name: /Подключить эту машину/ })).toBeNull();
  });
});
