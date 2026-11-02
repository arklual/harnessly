// When the hub asks a person to make an organization (HRN-536). The step shows once after the release, only when the
// instance has HUB_ORGS=on, the person owns a space and has no organization; «Позже» puts it off and nothing about
// their machines changes.
import type { Config, Me } from '../lib/types';

const LATER = 'hl-hub-org-later';

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

/** Whether `/` should lead to the step instead of the last space. */
export const orgStepDue = (config: Config, me: Me): boolean =>
  orgStepOn(config) && !orgStepDeferred() && me.spaces.some((s) => s.role === 'owner');
