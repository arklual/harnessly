// Organizations on the instance (HUB_ORGS=on) and where `/` takes a member of one.
import type { OrgList } from '../../hub-api/orgs';
import type { Config } from '../lib/types';

const LAST_ORG = 'hl-hub-last-org';

export const orgStepOn = (config: Config): boolean => (config as Config & { orgs?: string }).orgs === 'on';

/** Remembers the organization a person opened last, so `/` brings them back to it. */
export const rememberOrg = (slug: string) => {
  try {
    localStorage.setItem(LAST_ORG, slug);
  } catch {
    // not remembered: `/` opens the first organization
  }
};

const lastOrg = (): string | null => {
  try {
    return localStorage.getItem(LAST_ORG);
  } catch {
    return null;
  }
};

/** Where `/` takes a member of an organization: the last one they opened, else the first. Null without one. */
export const orgLanding = (list: OrgList | null): string | null => {
  const orgs = list?.orgs ?? [];
  if (orgs.length === 0) return null;
  const remembered = lastOrg();
  const org = orgs.find((o) => o.slug === remembered) ?? orgs[0];
  return org ? `/o/${org.slug}` : null;
};
