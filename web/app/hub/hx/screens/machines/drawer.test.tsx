// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { OrgApi } from '../../../org/api';
import { machinesMock } from '../../../org/mock/machines';
import type { Role } from '../../../org/types';

const state = vi.hoisted(() => ({ api: {} as unknown as OrgApi }));

vi.mock('../../../org/context', () => ({
  useOrg: () => ({
    api: state.api,
    scope: { kind: 'org' },
    shell: { org: { slug: 'acme' }, tenants: [{ id: 'x', slug: 'x', name: 'Other' }] },
  }),
}));

vi.mock('../../../lib/hooks', () => ({ useNow: () => Date.now() }));

import { MachineDrawer } from './drawer';

const open = async (role: Role) => {
  const api = machinesMock({ role }) as unknown as OrgApi;
  state.api = api;
  const list = await api.machinesList('acme', { scope: 'all' });
  const id = list.groups[0]?.machines[0]?.id ?? '';
  const setDlg = vi.fn();
  render(
    <MachineDrawer
      id={id}
      open
      hint={null}
      mlUrl={null}
      url={{ tab: null, dlg: null, close: vi.fn(), setTab: vi.fn(), setDlg }}
      onChanged={vi.fn()}
      onRemoved={vi.fn()}
    />,
  );
  return setDlg;
};

afterEach(() => cleanup());

describe('machine card: transfer to another tenant', () => {
  it('offers the transfer to a tenant admin and opens its dialog', async () => {
    const setDlg = await open('admin');
    const btn = await screen.findByRole('button', { name: /Перенести…/ });
    fireEvent.click(btn);
    expect(setDlg).toHaveBeenCalledWith('move');
  });

  it('does not offer it to someone who cannot administer the machine', async () => {
    await open('lead');
    await screen.findByText('Нагрузка за сутки');
    expect(screen.queryByRole('button', { name: /Перенести…/ })).toBeNull();
  });
});
