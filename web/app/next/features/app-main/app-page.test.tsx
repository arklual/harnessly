// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import AppPage, { abilitiesOf, type IAppPageProps } from '@/next/features/app-main/app-page';
import { OwnSwitch } from '@/next/features/app-main/app-switches';
import type { IInstalledApp } from '@/next/features/app-views/api';

afterEach(cleanup);

const app = (over: Partial<IInstalledApp> = {}): IInstalledApp => ({
  id: 'acme.demo',
  name: 'Демо',
  description: 'Что оно делает',
  version: '1.2.0',
  trust: 'sandboxed',
  targets: [],
  permissions: [],
  views: [],
  mcpServers: [],
  ...over,
});

const page = (over: Partial<IAppPageProps> = {}): IAppPageProps => ({
  name: 'Демо',
  line: 'Что оно делает',
  version: '1.2.0',
  by: 'ann',
  note: null,
  actions: <button type="button">Открыть</button>,
  preview: null,
  can: [],
  rights: <p>Права</p>,
  manage: null,
  journal: null,
  onBack: vi.fn(),
  onOpenDrawer: vi.fn(),
  ...over,
});

describe('abilitiesOf', () => {
  it('lists the screens, commands, buttons and the «@» search once each', () => {
    const lines = abilitiesOf(
      app({
        views: [
          { id: 'a', title: 'Сводка', targets: ['pane'] },
          { id: 'b', title: 'Сводка', targets: ['chat'] },
        ],
        commands: [{ name: 'sync', slash: '/x:sync', description: 'Обновить', kind: 'prompt' }],
        buttons: [{ id: 'go', title: 'Запустить' }],
        mentions: { title: 'Заказы', tool: 'find' },
      }),
    );
    expect(lines.filter((l) => l === 'Сводка')).toHaveLength(1);
    expect(lines).toContain('/sync — Обновить');
    expect(lines).toContain('Запустить');
    expect(lines).toHaveLength(4);
  });
});

describe('AppPage', () => {
  it('has the name as the heading, the actions, the rights and the version with the author', () => {
    render(<AppPage {...page()} />);
    expect(screen.getByRole('heading', { level: 1, name: 'Демо' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Открыть' })).toBeTruthy();
    expect(screen.getByText('Права')).toBeTruthy();
    expect(screen.getByText(/1\.2\.0/)).toBeTruthy();
    expect(screen.getByText(/ann/)).toBeTruthy();
  });

  it('draws no section with nothing in it', () => {
    render(<AppPage {...page()} />);
    expect(document.querySelector('.hx-apppage__preview')).toBeNull();
    expect(screen.queryByRole('tablist')).toBeNull();
  });

  it('has the journal as a second tab for a dev install', () => {
    render(<AppPage {...page({ journal: <p>Записи журнала</p> })} />);
    expect(screen.queryByText('Записи журнала')).toBeNull();
    fireEvent.click(screen.getByRole('tab', { name: /Журнал|Journal/ }));
    expect(screen.getByText('Записи журнала')).toBeTruthy();
    expect(screen.queryByText('Права')).toBeNull();
  });

  it('says so when the app is nowhere', () => {
    render(<AppPage {...page({ missing: true })} />);
    expect(screen.getByText(/Такого приложения нет|There is no such app/)).toBeTruthy();
  });

  it('goes back to the list', () => {
    const p = page();
    render(<AppPage {...p} />);
    fireEvent.click(screen.getByRole('button', { name: /Приложения|Apps/ }));
    expect(p.onBack).toHaveBeenCalled();
  });
});

describe('OwnSwitch (HRN-849)', () => {
  it('turns an app the person installed off with one switch', async () => {
    const onSwitch = vi.fn(async () => undefined);
    render(<OwnSwitch app={app()} onSwitch={onSwitch} />);
    fireEvent.click(screen.getByRole('switch', { name: /^(On|Включено)$/ }));
    await vi.waitFor(() =>
      expect(onSwitch).toHaveBeenCalledWith(expect.objectContaining({ id: 'acme.demo' }), false),
    );
  });
});
