import { describe, expect, it, vi } from 'vitest';

import { createOrgApi } from './api';
import { liveOrgApi } from './api/live';
import { navFor, sectionsOf } from './nav';

describe('menu by role', () => {
  const labels = (role: 'admin' | 'lead' | 'member') =>
    navFor(role, 'Тенант', 'Фронт').flatMap((g) => g.items.map((i) => i.label));

  it('gives the three roles different menus', () => {
    expect(labels('admin')).toEqual([
      'Обзор',
      'Аналитика',
      'Машины',
      'Команды',
      'Окружение',
      'Аккаунты и секреты',
      'Подключения',
    ]);
    expect(labels('lead')).toContain('Моя команда');
    expect(labels('lead')).not.toContain('Подключения');
    expect(labels('member')).toEqual(['Обзор', 'Аналитика', 'Окружение']);
  });

  it('leaves Окружение, Аккаунты и секреты, Подключения disabled', () => {
    const disabled = navFor('admin', 'Тенант', null)
      .flatMap((g) => g.items)
      .filter((i) => !i.section)
      .map((i) => i.label);
    expect(disabled).toEqual(['Окружение', 'Аккаунты и секреты', 'Подключения']);
  });

  it('opens fewer sections to a participant', () => {
    expect(sectionsOf('member')).toEqual(['overview', 'analytics']);
    expect(sectionsOf('admin')).toContain('teams');
  });
});

describe('the data layer switch', () => {
  it('serves the shell by role from mocks', async () => {
    const admin = await createOrgApi('mock', 'admin').shell('acme', 'all');
    const member = await createOrgApi('mock', 'member').shell('acme', 'product');
    expect(admin.viewer.role).toBe('admin');
    expect(admin.tenants.length).toBeGreaterThan(member.tenants.length);
    expect(member.teams.map((t) => t.id)).toEqual([member.viewer.teamId]);
  });

  it('tells a section the hub does not serve yet from one that is not there', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('{"error":"not_found"}', { status: 404 })),
    );
    await expect(liveOrgApi().shell('acme', 'all')).rejects.toMatchObject({ code: 'not_found' });
    await expect(liveOrgApi().overview('all', 'month')).rejects.toMatchObject({ code: 'not_ready' });
    vi.unstubAllGlobals();
  });

  it('rejects an unknown organization', async () => {
    await expect(createOrgApi('mock', 'admin').shell('nope', 'all')).rejects.toMatchObject({
      code: 'not_found',
    });
  });
});
