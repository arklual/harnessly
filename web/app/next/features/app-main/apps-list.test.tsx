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

describe('AppsList: consent and the switch (HRN-849)', () => {
  const files = {
    key: 'perm:files.write:src/**',
    code: 'files.write:src/**',
    class: 'medium' as const,
    label: { ru: 'Менять файлы src/**', en: 'Change the files src/**' },
  };
  const hooks = {
    key: 'part:hooks/',
    code: 'hooks/',
    class: 'critical' as const,
    label: { ru: 'Видеть действия агента и останавливать их', en: 'See the agent’s actions and stop them' },
    threat: {
      ru: 'Сможет видеть и останавливать действия агента.',
      en: 'It can see and stop what the agent does.',
    },
  };

  it('opens the install screen for an app that waits, and agrees to the version it showed', async () => {
    const onConsent = vi.fn(async () => undefined);
    const onOpen = vi.fn();
    render(
      <AppsList
        {...props([app({ consent: true, access: [files, hooks], state: 'waiting', reason: 'consent' })], {
          onConsent,
          onOpen,
          onDecline: vi.fn(async () => undefined),
        })}
      />,
    );
    fireEvent.click(screen.getByText('Демо'));
    expect(onOpen).not.toHaveBeenCalled();
    expect(screen.getByText(/(We do not check apps|Мы не проверяем приложения)/)).toBeTruthy();
    const items = [...document.querySelectorAll('.hx-access__item')].map((li) => li.textContent ?? '');
    expect(items[0]).toMatch(/(stop them|останавливать)/);
    fireEvent.click(screen.getByRole('button', { name: /^(Install|Установить)$/ }));
    await vi.waitFor(() =>
      expect(onConsent).toHaveBeenCalledWith(expect.objectContaining({ id: 'acme.demo' }), '1.2.0', [
        files.key,
        hooks.key,
      ]),
    );
  });

  it('shows an update that reaches more with only what it adds, marked new', () => {
    render(
      <AppsList
        {...props([app({ access: [files], pending: { version: '1.3.0', access: [hooks] } })], {
          onConsent: vi.fn(async () => undefined),
        })}
      />,
    );
    fireEvent.click(document.querySelector('.hx-appcard__more')!);
    fireEvent.click(
      screen.getByRole('button', { name: /^(An update waits for consent|Обновление ждёт согласия)$/ }),
    );
    const items = [...document.querySelectorAll('.hx-consent .hx-access__item')].map(
      (li) => li.textContent ?? '',
    );
    expect(items).toHaveLength(1);
    expect(items[0]).toMatch(/(new|новое)/);
  });

  it('turns an app the person installed off with one switch', async () => {
    const onSwitch = vi.fn(async () => undefined);
    render(<AppsList {...props([app({ access: [files] })], { onSwitch })} />);
    fireEvent.click(document.querySelector('.hx-appcard__more')!);
    fireEvent.click(screen.getByRole('switch', { name: /^(On|Включено)$/ }));
    await vi.waitFor(() =>
      expect(onSwitch).toHaveBeenCalledWith(expect.objectContaining({ id: 'acme.demo' }), false),
    );
  });
});
