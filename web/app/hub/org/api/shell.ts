import type { AppsConsent } from '../../../hub-api/orgs';
import type { ShellData } from '../types';

export interface ShellApi {
  /** The person, the organization, the tenants and the teams the shell draws the menu from. */
  shell(org: string, tenant: string): Promise<ShellData>;
  /** The organization's rule for apps on its machines. */
  appsConsent(org: string): Promise<AppsConsent>;
  /** Sets the rule; only the organization's admin may. Resolves with the rule the hub kept. */
  setAppsConsent(org: string, value: AppsConsent): Promise<AppsConsent>;
}
