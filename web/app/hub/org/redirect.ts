// The old space address of a tenant: a space that is a tenant of an organization lives in the organization
// hub, so `/s/<slug>…` leads there. Spaces with no organization keep the old screens. The decision is made on the hub's
// own answer (GET /api/orgs, GET /api/orgs/{org}), never on a guess from the address.
import type { OrgList, OrgView } from '../../hub-api/orgs';
import type { Config } from '../lib/types';
import { http, orgPath } from './api/live/http';
import { orgStepOn } from './step';
import type { Section } from './types';

export const sectionForSpaceRoute = (route: string, step?: string): Section | null => {
  if (route === '/s/:slug') return 'overview';
  if (route === '/s/:slug/settings') return 'teams';
  if (route === '/s/:slug/setup/:step' && step === 'machines') return 'machines';
  return null;
};

/** `/o/<org>/<tenant>/<section>` for the tenant a space is. */
export const orgAddress = (org: string, tenantSlug: string, section: Section): string =>
  `/o/${encodeURIComponent(org)}/${encodeURIComponent(tenantSlug)}/${section}`;

export interface TenantLookup {
  list: () => Promise<OrgList>;
  view: (org: string) => Promise<OrgView>;
}

const live: TenantLookup = {
  list: () => http.get<OrgList>('/api/orgs'),
  view: (org) => http.get<OrgView>(orgPath(org)),
};

/**
 * The organization a space is a tenant of, or null when it is a space of its own (or the hub cannot say). A space the
 * hub lists as loose is never looked up further.
 */
export const orgOfSpace = async (slug: string, lookup: TenantLookup = live): Promise<string | null> => {
  const { orgs, looseSpaces } = await lookup.list();
  if (looseSpaces.some((s) => s.slug === slug)) return null;
  for (const o of orgs) {
    const view = await lookup.view(o.slug).catch(() => null);
    if (view?.tenants.some((t) => t.slug === slug)) return o.slug;
  }
  return null;
};

/** Where an old address goes, or null when it stays: the instance has organizations off, the route has no new place, or
 * the space has no organization. A failed lookup also leaves the old screen, which works for every space. */
export const orgTargetFor = async (
  config: Config,
  route: string,
  slug: string,
  step?: string,
  lookup: TenantLookup = live,
): Promise<string | null> => {
  const section = sectionForSpaceRoute(route, step);
  if (!section || !orgStepOn(config)) return null;
  try {
    const org = await orgOfSpace(slug, lookup);
    return org ? orgAddress(org, slug, section) : null;
  } catch {
    return null;
  }
};
