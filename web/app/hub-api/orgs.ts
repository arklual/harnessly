// Organizations, tenants, teams, roles and the journal, as docs/hub/orgs.md writes them. Times are RFC 3339 UTC strings.

import type { SpaceRef } from '../hub/lib/types';

export type ScopeKind = 'org' | 'tenant' | 'team';
export type Scope = { kind: ScopeKind; id: string };
export type ScopeKey = `${ScopeKind}:${string}`;
export type GrantRole = 'admin' | 'lead';
export type Needs = 'org_admin' | 'tenant_admin' | 'team_lead' | 'instance_admin';
export type OrgsFlag = 'off' | 'on';

export type Org = {
  id: string;
  slug: string;
  name: string;
  timezone: string;
  currency: 'RUB' | 'USD';
  createdAt: string;
  /** The rule for apps on the organization's machines; absent on an older hub, which means `ask`. */
  appsConsent?: AppsConsent;
};

/** `ask`: an app's rights wait for the person's click; `auto`: «Не спрашивать разрешений». */
export type AppsConsent = 'ask' | 'auto';

export type Tenant = {
  id: string;
  slug: string;
  name: string;
  orgId: string;
  createdAt: string;
  memberCount: number;
};

export type Team = {
  id: string;
  tenantId: string;
  slug: string;
  name: string;
  createdAt: string;
  memberCount: number;
  leadIds: string[];
};

export type Grant = {
  userId: string;
  scope: Scope;
  role: GrantRole;
  grantedBy: string | null;
  grantedAt: string;
};

export type Capabilities = {
  view: boolean;
  money: boolean;
  manage: boolean;
  grant: GrantRole[];
  setLimit: boolean;
  setChildLimits: boolean;
  needs: { manage: Needs | null; setLimit: Needs | null };
};

export type Me = {
  userId: string;
  instanceAdmin: boolean;
  grants: Grant[];
  teams: { teamId: string; isPrimary: boolean }[];
  scopes: Record<string, Capabilities>; // key: ScopeKey
};

// features: the flags that are on for the organization (see FeaturesResponse for all of them and who decides).
export type OrgView = { org: Org; tenants: Tenant[]; teams: Team[]; me: Me; features: string[] };

// GET /api/orgs/{org}/features; PUT .../features/{name} answers { feature: FeatureView }.
export type FeatureView = {
  name: string;
  enabled: boolean;
  // forced: the instance decides the flag (HUB_FEATURES_FORCE), the organization's choice changes nothing.
  forced: boolean;
};
export type FeaturesResponse = { features: FeatureView[] };

export type OrgListItem = {
  id: string;
  slug: string;
  name: string;
  timezone: string;
  currency: 'RUB' | 'USD';
  createdAt: string;
  role: GrantRole | 'member'; // the person's highest role in it
};
export type OrgList = { orgs: OrgListItem[]; looseSpaces: SpaceRef[] };

export type Member = {
  userId: string;
  email: string;
  name: string;
  joinedAt: string;
  /** The latest of the sign-in to the hub and the work through a machine; null: never. */
  lastActiveAt: string | null;
  /** The last sign-in to the hub itself; null: works only through a machine so far. */
  signedInAt?: string | null;
  /** Always false for a member: they joined. Pending invitations are listed apart. */
  invited: boolean;
  /** Chats of the last seven days and the merge requests they ended with. */
  chatsWeek?: number;
  mrsWeek?: number;
  grants: Grant[];
  teams: { teamId: string; isPrimary: boolean }[];
  tenantRole: 'owner' | 'member' | null;
};

export type AuditAction =
  | 'org.create'
  | 'org.update'
  | 'tenant.create'
  | 'tenant.move'
  | 'tenant.delete'
  | 'machine.move'
  | 'team.create'
  | 'team.update'
  | 'team.delete'
  | 'member.add'
  | 'member.remove'
  | 'member.primary'
  | 'grant.add'
  | 'grant.remove'
  | 'limit.set'
  | 'limit.remove'
  | 'account.create'
  | 'account.update'
  | 'account.grant'
  | 'account.revoke'
  | 'machine.team'
  | 'machine.access'
  | 'machine.disable'
  | 'machine.enable'
  | 'machine.restart'
  | 'change.create'
  | 'signal.snooze'
  | 'signal.unsnooze';

export type AuditTargetKind =
  'user' | 'team' | 'tenant' | 'org' | 'limit' | 'account' | 'machine' | 'change' | 'signal';

export type AuditEvent = {
  id: string;
  at: string;
  actor: { userId: string; name: string } | null;
  scope: Scope;
  action: AuditAction;
  target: { kind: AuditTargetKind; id: string; label: string } | null;
  before: Record<string, unknown> | null;
  after: Record<string, unknown> | null;
  self: boolean;
};

export type AuditPage = { events: AuditEvent[]; next: string | null };

// Requests.
export type CreateOrgRequest = { name: string; spaceIds: string[] };
export type PatchOrgRequest = { name?: string; timezone?: string; currency?: 'RUB' | 'USD' };
export type CreateTeamRequest = { tenantId: string; name: string };
export type PutTeamMemberRequest = { isPrimary?: boolean };
export type PutGrantRequest = { userId: string; scope: Scope; role: GrantRole; granted: boolean };

// Error codes this part of the API adds to docs/hub/api.md's list.
export type OrgsErrorCode = 'forbidden_scope' | 'self_change' | 'last_admin';
export type OrgsErrorDetails = { scope?: ScopeKey; needs?: Needs; spaceId?: string };

export const scopeKey = (s: Scope): ScopeKey => `${s.kind}:${s.id}`;
