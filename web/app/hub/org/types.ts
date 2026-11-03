// Shared shapes of the organization hub: who is looking, at which scope, which sections they get. The data
// of each section lives next to its API (org/api/<section>.ts).

export type Role = 'admin' | 'lead' | 'member';
export type Section = 'overview' | 'analytics' | 'machines' | 'teams' | 'team' | 'env' | 'accounts';

export const ROLE_LABEL: Record<Role, string> = {
  admin: 'Админ организации',
  lead: 'Руководитель команды',
  member: 'Участник',
};

export interface Viewer {
  id: string;
  name: string;
  role: Role;
  /** The team a lead runs, or a member belongs to. */
  teamId: string | null;
}

export interface OrgRef {
  id: string;
  slug: string;
  name: string;
}

export interface TenantRef {
  id: string;
  slug: string;
  name: string;
  /** The place the person pinned it in the switcher. */
  pinned?: boolean;
  recent?: boolean;
}

export interface TeamRef {
  id: string;
  slug: string;
  name: string;
  leadName: string | null;
}

/** An organization the person belongs to (for the switcher's list). */
export interface OrgChoice {
  slug: string;
  name: string;
  roleLabel: string;
}

/** What the shell needs before it draws anything. */
export interface ShellData {
  viewer: Viewer;
  org: OrgRef;
  tenants: TenantRef[];
  /** The teams of the current tenant the person may see (a member: their own). */
  teams: TeamRef[];
  orgs: OrgChoice[];
  /** The organization's switched-on features (`org_features`): `ui.hx`, `env`, `catalog`… Absent on an older hub. */
  features?: string[];
}

/** `all` is the whole organization; anything else is a tenant slug. */
export const ALL_SCOPE = 'all';

export type Scope = { kind: 'org' } | { kind: 'tenant'; tenant: TenantRef };
