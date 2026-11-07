// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import AppsList from '@/next/features/app-main/apps-list';
import type { IInstalledApp } from '@/next/features/app-views/api';
import type { ITeamState } from '@/next/features/app-catalog/use-team';

afterEach(cleanup);

const app = (over: Partial<IInstalledApp> = {}): IInstalledApp => ({
  id: 'acme.demo',
  name: 'Демо',
  description: 'Что оно делает',
  version: '1.2.0',
  trust: 'sandboxed',
  targets: [],
  permissions: ['files.read'],
  views: [],
  mcpServers: [],
  ...over,
});

const props = (apps: IInstalledApp[] | null, extra: Partial<Parameters<typeof AppsList>[0]> = {}) => ({
  apps,
  error: null,
  onOpen: vi.fn(),
  onOpenDrawer: vi.fn(),
  onHelp: vi.fn(),
  ...extra,
});

const team = (role: 'owner' | 'member', muted: string[], act = vi.fn()): ITeamState => ({
  info: {
    space: 'team',
    role,
    muted,
    catalog: [
      {
        id: 'acme.demo',
        channel: 'stable',
        version: '1.2.0',
        name: 'Демо',
        description: '',
        trust: 'sandboxed',
        permissions: [],
        by: 'ann',
        installed: true,
      },
    ],
  },
  error: null,
  busy: null,
  failure: null,
  act,
});

describe('AppsList', () => {
  it('opens an app from its card', () => {
    const p = props([app()]);
    render(<AppsList {...p} />);
    fireEvent.click(screen.getByText('Что оно делает'));
    expect(p.onOpen).toHaveBeenCalledWith(expect.objectContaining({ id: 'acme.demo' }));
  });

  it('shows the empty state with no apps', () => {
    render(<AppsList {...props([])} />);
    expect(screen.getByText(/harnessly app install/)).toBeTruthy();
  });

  it('shows the rights in the side sheet', () => {
    render(<AppsList {...props([app()])} />);
    fireEvent.click(screen.getByRole('button', { name: /Подробности|Details/ }));
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByText('files.read')).toBeTruthy();
  });

  it('lets a member switch a team app off for themselves', () => {
    const act = vi.fn();
    render(<AppsList {...props([app()], { team: team('member', [], act) })} />);
    fireEvent.click(screen.getByRole('button', { name: /Подробности|Details/ }));
    fireEvent.click(screen.getByRole('switch', { name: /Включено у меня|On for me/ }));
    expect(act).toHaveBeenCalledWith(expect.objectContaining({ action: 'mute' }));
  });
});
