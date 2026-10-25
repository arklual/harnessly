// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { readView, type IPeersView } from '@/next/features/peers/api';
import PeersSection from '@/next/features/settings/peers-section';
import { allowedFor, parseWho, withRule } from '@/next/features/settings/peers';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const NOW = 1_800_000_000_000;

// GET <platform>/peers/settings as the runtime answers it (cmd/runtime/routes_peers.go).
const answer = {
  self: { person: 'peter@example.com', personName: 'Пётр', machine: 'runner-2', machineName: 'runner-2' },
  settings: { accept: true, scope: 'team', delivery: 'step', rules: [], rev: 3 },
  relay: true,
  senders: [
    {
      person: 'ivan@example.com',
      personName: 'Иван',
      machine: 'dev-department',
      machineName: 'dev-department',
    },
  ],
  audit: [
    {
      id: 'pm_1',
      at: NOW - 5 * 60_000,
      dir: 'in',
      from: {
        person: 'ivan@example.com',
        personName: 'Иван',
        machine: 'dev-department',
        machineName: 'dev-department',
        conversationId: 'tab-rsk',
        title: 'Модель по умолчанию',
      },
      to: {
        person: 'peter@example.com',
        personName: 'Пётр',
        machine: 'runner-2',
        machineName: 'runner-2',
        conversationId: 'tab-a',
        title: 'Релиз',
      },
      text: 'Баг: новый диалог',
      state: 'delivered',
      reason: '',
      via: 'agent',
    },
    {
      id: 'pm_2',
      at: NOW - 60 * 60_000,
      dir: 'in',
      from: {
        person: 'oleg@example.com',
        personName: 'Олег',
        machine: 'oleg-vm',
        machineName: 'oleg-vm',
        conversationId: '',
        title: '',
      },
      to: {
        person: '',
        personName: '',
        machine: 'runner-2',
        machineName: 'runner-2',
        conversationId: 'tab-a',
        title: '',
      },
      text: 'привет',
      state: 'refused',
      reason: 'not-allowed',
      via: 'agent',
    },
  ],
};

const view = (): IPeersView => readView(JSON.parse(JSON.stringify(answer)));

const show = (v: IPeersView | null, onChange = vi.fn(), busy: ReadonlySet<string> = new Set()) => {
  render(
    <PeersSection
      view={v}
      fleet={[{ id: 'local', name: 'runner-2' }]}
      now={NOW}
      busy={busy}
      onChange={onChange}
    />,
  );
  return onChange;
};

describe('the rules', () => {
  it('decides as the runtime: a rule first, then the scope', () => {
    const settings = view().settings;
    expect(allowedFor(settings, 'machine', 'dev-department')).toBe(true);
    const blocked = withRule(
      settings,
      { kind: 'machine', id: 'dev-department', name: 'dev-department' },
      false,
    );
    expect(allowedFor(blocked, 'machine', 'DEV-department')).toBe(false);
    expect(allowedFor({ ...settings, scope: 'list' }, 'person', 'ivan@example.com')).toBe(false);
    expect(parseWho(' ivan@example.com ')).toEqual({
      kind: 'person',
      id: 'ivan@example.com',
      name: 'ivan@example.com',
    });
    expect(parseWho('dev-department')?.kind).toBe('machine');
    expect(parseWho('a b')).toBeNull();
  });
});

describe('«Agent-to-agent messages» in Settings → Machines', () => {
  it('waits for the machine before it shows anything', () => {
    show(null);
    expect(screen.getByText('Agent-to-agent messages')).toBeTruthy();
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('shows this machine, everyone who wrote here, and the audit with the refusals in words', () => {
    show(view());
    for (const name of ['runner-2', 'dev-department', 'Иван'])
      expect(screen.getAllByText(name).length).toBeGreaterThan(0);
    expect(screen.getByText('this machine')).toBeTruthy();
    expect(
      (
        screen.getByRole('switch', { name: 'Allow dev-department to write to the agents' }) as HTMLElement
      ).getAttribute('aria-checked'),
    ).toBe('true');
    expect(
      screen.getByText('Иван · dev-department · Модель по умолчанию → Пётр · runner-2 · Релиз'),
    ).toBeTruthy();
    expect(screen.getByText('Олег · oleg-vm → runner-2')).toBeTruthy();
    expect(
      screen.getByText('they take messages only from their list, and this machine is not on it'),
    ).toBeTruthy();
  });

  it('blocks a machine, changes the scope and the delivery against the shown revision', () => {
    const onChange = show(view());
    fireEvent.click(screen.getByRole('switch', { name: 'Allow dev-department to write to the agents' }));
    expect(onChange).toHaveBeenLastCalledWith(
      'machine:dev-department',
      expect.objectContaining({
        rev: 3,
        rules: [{ kind: 'machine', id: 'dev-department', name: 'dev-department', allow: false }],
      }),
    );
    fireEvent.click(screen.getByRole('radio', { name: 'Only the list' }));
    expect(onChange).toHaveBeenLastCalledWith('scope', expect.objectContaining({ scope: 'list', rev: 3 }));
    fireEvent.click(screen.getByRole('radio', { name: 'After the turn' }));
    expect(onChange).toHaveBeenLastCalledWith('delivery', expect.objectContaining({ delivery: 'turn' }));
  });

  it('adds a person to the list by email', () => {
    const onChange = show(view());
    const add = screen.getByRole('button', { name: /Allow/ }) as HTMLButtonElement;
    expect(add.disabled).toBe(true);
    fireEvent.change(screen.getByRole('textbox', { name: 'Machine or email' }), {
      target: { value: 'oleg@example.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: /Allow/ }));
    expect(onChange).toHaveBeenLastCalledWith(
      'add',
      expect.objectContaining({
        rules: [{ kind: 'person', id: 'oleg@example.com', name: 'oleg@example.com', allow: true }],
      }),
    );
  });

  it('turned off, takes no rule changes, and says when the machine has no way to the others', () => {
    const v = view();
    v.settings.accept = false;
    v.relay = false;
    const onChange = show(v);
    fireEvent.click(screen.getByRole('switch', { name: 'Allow dev-department to write to the agents' }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText(/not connected to the other machines/)).toBeTruthy();
    fireEvent.click(screen.getByRole('switch', { name: 'Take messages from other agents' }));
    expect(onChange).toHaveBeenLastCalledWith('accept', expect.objectContaining({ accept: true }));
  });

  it('keeps a switch waiting while its change is on the way', () => {
    show(view(), vi.fn(), new Set(['machine:dev-department']));
    const row = screen
      .getByRole('switch', { name: 'Allow dev-department to write to the agents' })
      .closest('.hx-peers-set__who');
    expect(row?.getAttribute('data-busy')).toBe('true');
  });
});
