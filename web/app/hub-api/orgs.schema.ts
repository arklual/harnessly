// Runtime schemas of orgs.ts, for the fixture test. contracts.test.ts asserts each one infers exactly its type.

import { arr, bool, lit, nullable, num, obj, opt, rec, str, time, unknownValue } from './schema';

const scopeKind = lit('org', 'tenant', 'team');
export const scope = obj({ kind: scopeKind, id: str });
const grantRole = lit('admin', 'lead');
const needs = nullable(lit('org_admin', 'tenant_admin', 'team_lead', 'instance_admin'));

export const org = obj({
  id: str,
  slug: str,
  name: str,
  timezone: str,
  currency: lit('RUB', 'USD'),
  createdAt: time,
});
export const tenant = obj({ id: str, slug: str, name: str, orgId: str, createdAt: time, memberCount: num });
export const team = obj({
  id: str,
  tenantId: str,
  slug: str,
  name: str,
  createdAt: time,
  memberCount: num,
  leadIds: arr(str),
});
export const grant = obj({ userId: str, scope, role: grantRole, grantedBy: nullable(str), grantedAt: time });
export const capabilities = obj({
  view: bool,
  money: bool,
  manage: bool,
  grant: arr(grantRole),
  setLimit: bool,
  setChildLimits: bool,
  needs: obj({ manage: needs, setLimit: needs }),
});
const teamRef = obj({ teamId: str, isPrimary: bool });
export const me = obj({
  userId: str,
  instanceAdmin: bool,
  grants: arr(grant),
  teams: arr(teamRef),
  scopes: rec(capabilities, /^(org|tenant|team):[a-z2-7]{20}$/),
});
export const orgView = obj({ org, tenants: arr(tenant), teams: arr(team), me, features: arr(str) });

export const featureView = obj({ name: str, enabled: bool, forced: bool });
export const featuresResponse = obj({ features: arr(featureView) });

const spaceRef = obj({ id: str, slug: str, name: str, role: lit('owner', 'member') });
export const orgList = obj({
  orgs: arr(
    obj({
      id: str,
      slug: str,
      name: str,
      timezone: str,
      currency: lit('RUB', 'USD'),
      createdAt: time,
      role: lit('admin', 'lead', 'member'),
    }),
  ),
  looseSpaces: arr(spaceRef),
});

export const member = obj({
  userId: str,
  email: str,
  name: str,
  joinedAt: time,
  lastActiveAt: nullable(time),
  signedInAt: opt(nullable(time)),
  invited: bool,
  chatsWeek: opt(num),
  mrsWeek: opt(num),
  grants: arr(grant),
  teams: arr(teamRef),
  tenantRole: nullable(lit('owner', 'member')),
});
export const members = obj({ members: arr(member) });

export const auditEvent = obj({
  id: str,
  at: time,
  actor: nullable(obj({ userId: str, name: str })),
  scope,
  action: lit(
    'org.create',
    'org.update',
    'tenant.move',
    'team.create',
    'team.update',
    'team.delete',
    'member.add',
    'member.remove',
    'member.primary',
    'grant.add',
    'grant.remove',
    'limit.set',
    'limit.remove',
    'account.create',
    'account.update',
    'account.grant',
    'account.revoke',
    'machine.team',
    'machine.access',
    'machine.disable',
    'machine.enable',
    'machine.restart',
    'change.create',
    'signal.snooze',
  ),
  target: nullable(
    obj({
      kind: lit('user', 'team', 'tenant', 'org', 'limit', 'account', 'machine', 'change', 'signal'),
      id: str,
      label: str,
    }),
  ),
  before: nullable(rec(unknownValue)),
  after: nullable(rec(unknownValue)),
  self: bool,
});
export const auditPage = obj({ events: arr(auditEvent), next: nullable(str) });
