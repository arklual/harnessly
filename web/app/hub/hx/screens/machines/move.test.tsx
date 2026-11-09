// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { MovePlan, OrgMachine } from '../../../org/api/machines';

const state = vi.hoisted(() => ({ api: {} as Record<string, unknown> }));

vi.mock('../../../org/context', () => ({
  useOrg: () => ({
    api: state.api,
    shell: {
      tenants: [
        { id: 'a', slug: 'a', name: 'Alpha' },
        { id: 'b', slug: 'b', name: 'Beta' },
      ],
    },
  }),
}));

import { Move } from './dialogs';

const machine = { id: 'm1', name: 'build-1', tenantId: 'a', canAdmin: true } as unknown as OrgMachine;
const plan: MovePlan = {
  from: { id: 'a', name: 'Alpha' },
  to: { id: 'b', name: 'Beta' },
  noop: false,
  teamCleared: 'Sales',
  teamAssigned: null,
  accessRevoked: ['Ann'],
  limits: { fromRules: 0, toRules: 0 },
  kept: ['id', 'key'],
} as MovePlan;

afterEach(() => cleanup());

const pickBeta = () => fireEvent.change(screen.getByLabelText('Куда перенести'), { target: { value: 'b' } });

describe('Move dialog of the new machine card', () => {
  it('asks for the dry-run plan, shows its consequences, and moves only after that', async () => {
    const order: string[] = [];
    state.api = {
      planMoveMachine: vi.fn(async () => (order.push('plan'), plan)),
      moveMachine: vi.fn(async () => (order.push('move'), plan)),
    };
    const onMoved = vi.fn();
    render(<Move m={machine} onClose={vi.fn()} onMoved={onMoved} />);
    expect((screen.getByRole('button', { name: 'Перенести' }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText(/сохранит id и ключ/)).toBeTruthy();
    pickBeta();
    await screen.findByText(/Привязка к команде «Sales» снимется/);
    expect(screen.getByText(/Доступ потеряют: Ann/)).toBeTruthy();
    expect(state.api.moveMachine).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Перенести' }));
    await waitFor(() => expect(onMoved).toHaveBeenCalled());
    expect(order).toEqual(['plan', 'move']);
    expect(state.api.planMoveMachine).toHaveBeenCalledWith('m1', 'b');
    expect(state.api.moveMachine).toHaveBeenCalledWith('m1', 'b');
  });

  it('does not offer the current tenant and blocks a no-op plan', async () => {
    state.api = {
      planMoveMachine: vi.fn(async () => ({ ...plan, noop: true })),
      moveMachine: vi.fn(),
    };
    render(<Move m={machine} onClose={vi.fn()} onMoved={vi.fn()} />);
    expect(screen.queryByRole('option', { name: 'Alpha' })).toBeNull();
    pickBeta();
    await screen.findByText('Машина уже в этом тенанте.');
    expect((screen.getByRole('button', { name: 'Перенести' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('shows the hub refusal of the move and stays open', async () => {
    state.api = {
      planMoveMachine: vi.fn(async () => plan),
      moveMachine: vi.fn(async () => {
        throw new Error('В тенанте нет места.');
      }),
    };
    const onMoved = vi.fn();
    render(<Move m={machine} onClose={vi.fn()} onMoved={onMoved} />);
    pickBeta();
    await screen.findByText(/Привязка к команде/);
    fireEvent.click(screen.getByRole('button', { name: 'Перенести' }));
    await screen.findByRole('alert');
    expect(onMoved).not.toHaveBeenCalled();
  });

  it('shows a refused plan and keeps the move disabled', async () => {
    state.api = {
      planMoveMachine: vi.fn(async () => {
        throw new Error('Вы не админ тенанта Beta.');
      }),
      moveMachine: vi.fn(),
    };
    render(<Move m={machine} onClose={vi.fn()} onMoved={vi.fn()} />);
    pickBeta();
    await screen.findByRole('alert');
    expect((screen.getByRole('button', { name: 'Перенести' }) as HTMLButtonElement).disabled).toBe(true);
    expect(state.api.moveMachine).not.toHaveBeenCalled();
  });
});
