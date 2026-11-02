import type { ShellData } from '../types';

export interface ShellApi {
  /** The person, the organization, the tenants and the teams the shell draws the menu from. */
  shell(org: string, tenant: string): Promise<ShellData>;
}
