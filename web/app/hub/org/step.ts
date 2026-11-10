// When the hub asks a person to make an organization. The step shows once after the release, only when the
// instance has HUB_ORGS=on, the person owns a space and has no organization; «Позже» puts it off and nothing about
// their machines changes.
import type { OrgList } from '../../hub-api/orgs';
import type { Config, Me } from '../lib/types';

const LATER = 'hl-hub-org-later';
const LAST_ORG = 'hl-hub-last-org';

export const orgStepOn = (config: Config): boolean => (config as Config & { orgs?: string }).orgs === 'on';

export const orgStepDeferred = (): boolean => {
  try {
    return localStorage.getItem(LATER) !== null;
  } catch {
    return false;
  }
};

export const deferOrgStep = () => {
  try {
    localStorage.setItem(LATER, new Date().toISOString());
  } catch {
    // not remembered: the step shows again next time
  }
};

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

/** Whether `/` should lead to the step instead of the last space: only a person in no organization yet is asked. */
export const orgStepDue = (config: Config, me: Me, list: OrgList | null = null): boolean =>
  orgStepOn(config) &&
  !orgStepDeferred() &&
  (list === null || list.orgs.length === 0) &&
  me.spaces.some((s) => s.role === 'owner');

/** Where `/` takes a member of an organization: the last one they opened, else the first. Null without one. */
export const orgLanding = (list: OrgList | null): string | null => {
  const orgs = list?.orgs ?? [];
  if (orgs.length === 0) return null;
  const remembered = lastOrg();
  const org = orgs.find((o) => o.slug === remembered) ?? orgs[0];
  return org ? `/o/${org.slug}` : null;
};
