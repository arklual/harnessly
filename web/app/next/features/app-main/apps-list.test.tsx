// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AppsList from '@/next/features/app-main/apps-list';
import type { IInstalledApp } from '@/next/features/app-views/api';

beforeEach(() => {
  // jsdom has no matchMedia (the menu and the toast ask which frame they are in).
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
});
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
  onCreate: vi.fn(),
  query: '',
  onQuery: vi.fn(),
  ...extra,
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

  it('says in words that an app waits for the word of the person, and nothing else', () => {
    render(
      <AppsList
        {...props([
          app({ consent: true, state: 'waiting', reason: 'consent' }),
          app({ id: 'a.b', name: 'Ок' }),
        ])}
      />,
    );
    expect(screen.getAllByText(/ждёт вашего согласия|waits for your consent/i).length).toBe(1);
  });
});
