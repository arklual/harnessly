// Fixtures for the shell: the organization «Акме», its tenants and teams, and the three people who look at them.
import type { ShellApi } from '../api/shell';
import { OrgApiError } from '../api/errors';
import type { OrgChoice, TeamRef, TenantRef, Viewer } from '../types';
import { later, type MockCtx } from './ctx';
import { renamedTenants } from './tenant';

export const TENANTS: TenantRef[] = [
  { id: 't-product', slug: 'product', name: 'Продукт', recent: true },
  { id: 't-platform', slug: 'platform', name: 'Платформа', recent: true },
  { id: 't-clients', slug: 'clients', name: 'Клиенты', recent: true },
  { id: 't-billing', slug: 'billing', name: 'Биллинг', pinned: true },
  { id: 't-scanner', slug: 'scanner', name: 'Сканер', pinned: true },
];

export const TEAMS: TeamRef[] = [
  { id: 'tm-front', slug: 'front', name: 'Фронт', leadName: 'Аня' },
  { id: 'tm-back', slug: 'back', name: 'Бэкенд', leadName: 'Дима' },
  { id: 'tm-design', slug: 'design', name: 'Дизайн', leadName: 'Лиза' },
];

const ORGS: OrgChoice[] = [
  { slug: 'acme', name: 'Акме', roleLabel: 'владелец' },
  { slug: 'harnessly', name: 'Харнесли', roleLabel: 'владелец' },
];

export const VIEWERS: Record<'admin' | 'lead' | 'member', Viewer> = {
  admin: { id: 'u-andrey', name: 'Андрей', role: 'admin', teamId: null },
  lead: { id: 'u-anya', name: 'Андрей', role: 'lead', teamId: 'tm-front' },
  member: { id: 'u-andrey-m', name: 'Андрей', role: 'member', teamId: 'tm-front' },
};

/** The mock organization has every feature on, so the gallery shows the whole new hub. */
export const MOCK_FEATURES = [
  'ui.hx',
  'env',
  'catalog',
  'machine.resources',
  'org.events',
  'ui.hx.live',
  'secrets.vault',
];

export const shellMock = (ctx: MockCtx): ShellApi => ({
  shell: async (org, tenant) => {
    const known = ORGS.find((o) => o.slug === org);
    if (!known) throw new OrgApiError('not_found', 'organization not found');
    if (tenant !== 'all' && !TENANTS.some((t) => t.slug === tenant)) {
      throw new OrgApiError('not_found', 'tenant not found');
    }
    const viewer = VIEWERS[ctx.role];
    return later({
      viewer,
      org: { id: `org-${known.slug}`, slug: known.slug, name: known.name },
      tenants: (ctx.role === 'admin' ? TENANTS : TENANTS.slice(0, 1)).map((t) => ({
        ...t,
        name: renamedTenants.get(t.id) ?? t.name,
      })),
      teams: ctx.role === 'member' ? TEAMS.filter((t) => t.id === viewer.teamId) : TEAMS,
      orgs: ORGS,
      features: MOCK_FEATURES,
    });
  },
});
