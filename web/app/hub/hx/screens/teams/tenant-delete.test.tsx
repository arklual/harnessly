// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { TenantDeletePlan } from '../../../org/api/tenant';
import type { TenantRef } from '../../../org/types';

const state = vi.hoisted(() => ({ api: {} as Record<string, unknown> }));
const refreshShell = vi.hoisted(() => vi.fn());

vi.mock('../../../org/context', () => ({
  useOrg: () => ({ api: state.api, shell: { org: { slug: 'acme' } }, refreshShell }),
}));

import { ConfirmDialog } from './confirm';
import { Delete, useTenantDelete } from './tenant';

const tenant = { id: 'sp1', slug: 'sp1', name: 'Alpha' } as unknown as TenantRef;

const Harness = ({ onDone }: { onDone: () => void }) => {
  const del = useTenantDelete(tenant, onDone);
  return (
    <>
      <Delete del={del} />
      <ConfirmDialog
        open={del.asking}
        label="Удалить тенант"
        title="Удалить?"
        lead="x"
        yes="Да, удалить"
        busy={del.deleting}
        error={del.error}
        onNo={() => del.setAsking(false)}
        onYes={() => void del.remove()}
      />
    </>
  );
};

afterEach(() => cleanup());

describe('deleting a tenant in the new UI', () => {
  it('lists the blockers of a non-empty tenant and never calls deleteTenant', async () => {
    const plan: TenantDeletePlan = {
      blockers: [
        { kind: 'machines', count: 2 },
        { kind: 'members', count: 3 },
      ],
      removes: [],
      canDelete: false,
    };
    state.api = { planDeleteTenant: vi.fn(async () => plan), deleteTenant: vi.fn() };
    render(<Harness onDone={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Проверить, можно ли удалить' }));
    await screen.findByText('Мешает: 2 машин');
    expect(screen.getByText('Мешает: 3 участников')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Удалить тенант/ })).toBeNull();
    expect(state.api.planDeleteTenant).toHaveBeenCalledWith('acme', 'sp1');
    expect(state.api.deleteTenant).not.toHaveBeenCalled();
  });

  it('deletes an empty tenant only after the plan allows it and the person confirms', async () => {
    const plan: TenantDeletePlan = {
      blockers: [],
      removes: [{ kind: 'limit_rules', count: 1 }],
      canDelete: true,
    };
    state.api = { planDeleteTenant: vi.fn(async () => plan), deleteTenant: vi.fn(async () => undefined) };
    const onDone = vi.fn();
    render(<Harness onDone={onDone} />);
    expect(state.api.deleteTenant).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Проверить, можно ли удалить' }));
    await screen.findByText(/Уйдёт вместе с ним: 1 правил лимитов/);
    expect(state.api.deleteTenant).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: /Удалить тенант…/ }));
    fireEvent.click(await screen.findByRole('button', { name: 'Да, удалить' }));
    await waitFor(() => expect(onDone).toHaveBeenCalled());
    expect(state.api.deleteTenant).toHaveBeenCalledWith('acme', 'sp1');
    expect(refreshShell).toHaveBeenCalled();
  });

  it('shows the hub refusal when the deletion fails', async () => {
    state.api = {
      planDeleteTenant: vi.fn(async () => ({ blockers: [], removes: [], canDelete: true })),
      deleteTenant: vi.fn(async () => {
        throw new Error('В тенанте остались машины.');
      }),
    };
    const onDone = vi.fn();
    render(<Harness onDone={onDone} />);
    fireEvent.click(screen.getByRole('button', { name: 'Проверить, можно ли удалить' }));
    fireEvent.click(await screen.findByRole('button', { name: /Удалить тенант…/ }));
    fireEvent.click(await screen.findByRole('button', { name: 'Да, удалить' }));
    await screen.findByRole('alert');
    expect(onDone).not.toHaveBeenCalled();
  });
});
