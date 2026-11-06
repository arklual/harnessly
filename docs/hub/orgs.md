# Organizations, tenants, teams: the contract

Part of the [hub contract](api.md); the same conventions (JSON, `camelCase`, RFC 3339 UTC times, the error body). TS
types: `web/app/hub-api/orgs.ts`; mock fixtures: `web/app/hub-api/fixtures/orgs/*.json`, checked against the types by
`web/app/hub-api/contracts.test.ts`. When code and this file disagree one of them is wrong.

## Model

```
organization ⊃ tenant ⊃ team ⊃ person
```

- **Organization** — `orgs`. Has a time zone (days and months are cut in it) and a display currency (amounts are kept in
  USD and shown in it at the month's rate, see [analytics.md](analytics.md)).
- **Tenant** — an existing space (`spaces`) with `org_id` set. A space without one is a tenant of an **implicit
  organization** of its own: nothing about it changes (owner = the tenant's admin, machines and agents work by
  `space_id` as before). The new endpoints never list implicit organizations; `GET /api/orgs` returns such spaces as
  `looseSpaces`.
- **Team** — inside a tenant. A person may be in several teams of a tenant; one of them is primary.
- **Membership** of a tenant stays `memberships` (`owner`/`member`). Roles above that are **grants**: `admin` of an
  organization or a tenant, `lead` of a team. An `owner` of a space counts as the tenant's admin.

A scope is written `kind:id` in query strings (`org:k3…`, `tenant:a7…`, `team:q2…`) and `{ kind, id }` in bodies.

## Flag

`HUB_ORGS` (`GET /api/config` → `orgs`):

| Value | The endpoints below |
| --- | --- |
| `off` | answer 404 `not_found`, as if they did not exist |
| `shadow` | answer instance administrators only (`User.isAdmin`); anyone else gets 404 |
| `on` | answer everyone, by the rights below |

## Rights

One rule. A person **manages** scope S when they are `admin` of S or of an ancestor of S, or `lead` of S when S is their
team (organization ⊃ tenant ⊃ team ⊃ person). Managing means: membership, roles at S and below, the limits of S's
*children*, machines and access to them, accounts in S. Analytics and money can be seen in one's own scope and below;
a member sees only their own.

The table, word for word from the plan, and the endpoint each row is enforced on:

| Действие | Кто | Ручка |
| --- | --- | --- |
| Админ организации: назначить, снять | админ организации | `PUT /api/orgs/{org}/grants` with `scope.kind = 'org'`, `role = 'admin'` |
| Админ тенанта | админ тенанта и выше | `PUT /api/orgs/{org}/grants` with `scope.kind = 'tenant'`, `role = 'admin'` |
| Лид команды | лид этой команды и выше | `PUT /api/orgs/{org}/grants` with `scope.kind = 'team'`, `role = 'lead'` |
| Лимит тенанта | админ организации (не админ тенанта) | `PUT /api/orgs/{org}/limits` with `scope.kind = 'tenant'` ([limits.md](limits.md)) |
| Лимит команды | админ тенанта и выше (не лид) | `PUT /api/orgs/{org}/limits` with `scope.kind = 'team'` |
| Личный лимит в команде | лид команды и выше | `PUT /api/orgs/{org}/limits` with `scope.kind = 'team'` and `userId` |
| Личный потолок в тенанте, личный лимит лида | админ тенанта и выше | `PUT /api/orgs/{org}/limits` with `scope.kind = 'tenant'` and `userId`; a lead's own personal limit in their team likewise needs the tenant's admin |
| Лимит организации, лимит на аккаунт организации | админ организации | `PUT /api/orgs/{org}/limits` with `scope.kind = 'org'`, or with `accountId` of an organization's account |

The holes it closes, each a test of `internal/hub/authz`:

- **Not oneself** (`403 self_change`). Nobody changes their own role, their own limit or the limit of a scope they
  manage themselves: a tenant's admin does not raise the tenant's limit, a lead does not raise the team's budget. The
  one exception is an organization's admin setting the organization's limit; that entry is written with `self: true`
  and every admin of the organization sees it in the journal.
- **Removing** is allowed for anyone at one's level and below, never above (`403 forbidden_scope`). The last admin of
  an organization cannot be removed (`409 last_admin`).
- **A person in two teams.** A session belongs to one team: the machine's team when the person is in it, otherwise the
  person's primary team. The lead of team B neither sees nor limits what went to team A. A personal ceiling in the
  tenant limits everything.
- **A lead handing out more than the budget.** Personal limits are ceilings, not shares: their sum may exceed the team's
  budget; both bounds act and the tightest wins. The UI warns «раздано 120%» (`Team.limitsHandedOutPct`).
- **One key for several tenants.** An account belongs to a scope and is granted to its descendants
  (`account_grants`). A tenant's admin may add «account X in my tenant» — a rule of its own, not an edit of the shared
  one. A tenant does not see others' spend on the key, only the window's remainder.
- **One key, different limits per person** — a rule with `userId` and `accountId`.
- **A tenant moved between organizations** — only by an admin of both. The old organization's limits are dropped; the
  journal stays.

Every change of roles, membership, limits, accounts, access and environment is written to the journal with «было» and
«стало», in the transaction of the change.

## Errors (in addition to [api.md](api.md#errors))

| HTTP | `error` | When |
| --- | --- | --- |
| 403 | `forbidden_scope` | signed in and in the organization, but the action is above one's level or outside one's scope (`details.scope`, `details.needs`: the role that could do it, e.g. `"tenant_admin"`) |
| 403 | `self_change` | the change is about oneself or about a scope one manages oneself (`details.needs` as above) |
| 409 | `last_admin` | the last admin of the organization would be removed |
| 404 | `not_found` | the organization does not exist, the person has nothing in it, or `HUB_ORGS` hides the endpoint |

`details.needs` is one of `org_admin`, `tenant_admin`, `team_lead`, `instance_admin`: the UI's hint «Может поменять:
админ тенанта» comes from it, and from `Capabilities.needs` before any request.

## Types

```ts
type ScopeKind = 'org' | 'tenant' | 'team';
type Scope = { kind: ScopeKind; id: string };
type GrantRole = 'admin' | 'lead';            // admin: org or tenant; lead: team
type Needs = 'org_admin' | 'tenant_admin' | 'team_lead' | 'instance_admin';

type Org = {
  id: string; slug: string; name: string;
  timezone: string;                           // IANA, default 'Europe/Moscow'
  currency: 'RUB' | 'USD';                    // display currency; money is kept in USD
  createdAt: string;
};

type Tenant = { id: string; slug: string; name: string; orgId: string; createdAt: string; memberCount: number };
type Team = {
  id: string; tenantId: string; slug: string; name: string; createdAt: string;
  memberCount: number;
  leadIds: string[];
};

type Grant = { userId: string; scope: Scope; role: GrantRole; grantedBy: string | null; grantedAt: string };

// What the signed-in person may do at one scope. The UI disables what is false and shows `needs` as the hint.
type Capabilities = {
  view: boolean;              // see the scope's people and analytics
  money: boolean;             // see the scope's money (spend, subscriptions)
  manage: boolean;            // membership, roles below, machines, accounts
  grant: GrantRole[];         // roles they may give and take at this scope
  setLimit: boolean;          // set the limit OF this scope (not of its children)
  setChildLimits: boolean;    // set the limits of the scope's children and of the people in it
  needs: { manage: Needs | null; setLimit: Needs | null }; // who could, when they cannot (null when they can)
};

type Me = {
  userId: string;
  instanceAdmin: boolean;
  grants: Grant[];                            // the person's own grants in this organization
  teams: { teamId: string; isPrimary: boolean }[];
  scopes: Record<string, Capabilities>;       // key 'org:<id>' | 'tenant:<id>' | 'team:<id>', every scope of the org they can view
};

type OrgView = { org: Org; tenants: Tenant[]; teams: Team[]; me: Me };

type Member = {
  userId: string; email: string; name: string;
  joinedAt: string;                           // into the tenant (or the earliest of its tenants, for the org scope)
  lastActiveAt: string | null;
  invited: boolean;                           // a pending invitation, no account yet
  grants: Grant[];                            // at this scope and below
  teams: { teamId: string; isPrimary: boolean }[];
  tenantRole: 'owner' | 'member' | null;      // membership of the tenant (null for an org-only admin)
};

type AuditAction =
  | 'org.create' | 'org.update' | 'tenant.move'
  | 'team.create' | 'team.update' | 'team.delete'
  | 'member.add' | 'member.remove' | 'member.primary'
  | 'grant.add' | 'grant.remove'
  | 'limit.set' | 'limit.remove'
  | 'account.create' | 'account.update' | 'account.grant' | 'account.revoke'
  | 'machine.team' | 'machine.access' | 'machine.disable' | 'machine.enable' | 'machine.restart'
  | 'change.create' | 'signal.snooze' | 'signal.unsnooze';

type AuditEvent = {
  id: string;
  at: string;
  actor: { userId: string; name: string } | null;  // null: the hub itself (seed, a job)
  scope: Scope;
  action: AuditAction;
  target: { kind: 'user' | 'team' | 'tenant' | 'org' | 'limit' | 'account' | 'machine' | 'change' | 'signal'; id: string; label: string } | null;
  before: Record<string, unknown> | null;     // «было»
  after: Record<string, unknown> | null;      // «стало»
  self: boolean;                              // a change of one's own scope (only an org admin on the org's limit)
};
```

## Endpoints

All need a session; `{org}` is the organization's id or slug.

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET /api/orgs` | any | | `{ orgs: (Org & { role: GrantRole \| 'member' })[], looseSpaces: SpaceRef[] }` — organizations the person has anything in (a grant, a tenant membership); in `shadow` an instance admin gets every organization. `looseSpaces`: their spaces with no organization (the onboarding step offers to move those they own) |
| `POST /api/orgs` | any (`shadow`: instance admin) | `{ name, spaceIds: string[], tenantName?: string }` (name 1–60; `spaceIds` may be empty; `tenantName` makes the first tenant, owned by the author: empty string means «Основное», absent means none, as before) | 201 `OrgView` — makes the organization, makes the author its admin, and moves the listed spaces into it: each must be a space the author owns and in no organization, else 403 `forbidden_scope` (`details.spaceId`) and nothing is made. One transaction, journal `org.create` and `tenant.move` per space |
| `GET /api/orgs/{org}` | anyone with something in it | | `OrgView` — tenants and teams the person can view (a member: their tenant and their teams only) |
| `PATCH /api/orgs/{org}` | org admin | `{ name?, timezone?, currency? }` | `{ org: Org }` |
| `POST /api/orgs/{org}/tenants/move` | org admin who owns the space (or admin of the space's organization too) | `{ spaceId }` | `{ tenant: Tenant }`; journal `tenant.move` in both organizations |
| `GET /api/orgs/{org}/teams?tenant=<id>` | view of the tenant | | `{ teams: Team[] }` |
| `POST /api/orgs/{org}/teams` | tenant admin and above | `{ tenantId, name }` | 201 `{ team: Team }` (slug from the name like spaces) |
| `PATCH /api/orgs/{org}/teams/{teamId}` | lead of the team and above | `{ name }` | `{ team: Team }` |
| `DELETE /api/orgs/{org}/teams/{teamId}` | tenant admin and above | | 204; its machines keep their tenant and lose the team |
| `GET /api/orgs/{org}/scopes/{kind}/{id}/members` | view of the scope | | `{ members: Member[] }` — org: everyone in its tenants and its admins; tenant: its members; team: its people. A member asking about a scope above their own team gets 403 `forbidden_scope` |
| `PUT /api/orgs/{org}/teams/{teamId}/members/{userId}` | manage of the team (lead and above) | `{ isPrimary?: boolean }` | `{ member: Member }` — adds a member of the tenant to the team (400 `invalid_request` `details.field = 'userId'` when not a tenant member); `isPrimary: true` makes it their primary team in that tenant (tenant admin and above: the plan's «основную выбирает админ тенанта») |
| `DELETE /api/orgs/{org}/teams/{teamId}/members/{userId}` | manage of the team | | 204 |
| `PUT /api/orgs/{org}/grants` | per the rights table | `{ userId, scope: Scope, role: GrantRole, granted: boolean }` | `{ grant: Grant \| null }` — `granted: false` removes. 403 `self_change` (one's own role), 403 `forbidden_scope` (above one's level), 409 `last_admin`. Granting a tenant role to someone outside the tenant also adds them as a member; a team lead must be a member of the tenant |
| `GET /api/orgs/{org}/audit?scope=<kind:id>&before=<RFC 3339>&limit=50` | view of the scope | | `{ events: AuditEvent[], next: string \| null }` — the scope's events and its descendants', newest first; never a neighbour's. `next` is the `before` for the next page (opaque: the last event's time and id, so events of one instant are never split by a page). A member gets only events whose target is themselves |

Machine endpoints that take a team (`PATCH /api/machines/{id}` `{ teamId }`, disable, enable, restart) are in
[machines in the new hub](analytics.md#machines).

## Seeding (`hub seed`)

`hub seed --file seed.yaml [--dry-run]` makes organizations, tenants, teams and people from a file, in one transaction,
upserting by natural keys: `orgs.slug`, `(org, tenant.slug)`, `(tenant, team.slug)`, `users.email` and the grant. A second
run changes nothing; `--dry-run` prints what would change («изменений нет» when nothing). A person who never signed in
gets a `users` row by email; signing in by the email code later joins it (a proved address, see architecture.md,
«Identities»).

```yaml
orgs:
  - slug: acme
    name: Acme
    timezone: Europe/Moscow   # optional
    currency: RUB             # optional
    admins: [anna@example.com]
    tenants:
      - slug: acme            # an existing space with this slug is moved in; otherwise one is made
        name: Acme
        owners: [anna@example.com]
        members: [boris@example.com]
        teams:
          - slug: general
            name: Общая
            leads: []
            members: [anna@example.com, boris@example.com]   # the first team of a person in a tenant is primary
```

`offboard:` takes people out: every role, tenant and team membership and every session of the listed addresses goes,
the `users` row stays (usage and audit point at it), and signing in again opens nothing. The last owner of a tenant is
refused (name another owner first), and an address both seeded and offboarded in one file is an error.

```yaml
offboard: [old-address@example.com]
```

`accounts:` under an organization registers the model accounts its machines report (docs/hub/usage.md, `accountRef`):
a machine names an account it has not seen before personal, and only an organization's accounts count as its money.
Upserted by `(org, provider, ref)`; the account stops being `discovered`, its sessions take its funding and the hours of
their calls are summed anew. `history: true` also gives it the owner's sessions of that CLI that came with no account (a
machine sends the account only for sessions started after it first saw it); a later record of such a session that names
no account keeps the one given.

```yaml
    accounts:
      - provider: claude          # the CLI: claude or codex
        ref: claude:<account uuid> # as the machine reports it: claude:<uuid>, codex:<id>, key:<hash>
        label: Claude Max (Anna)
        plan: Max 20x
        funding: org              # org (default) or personal
        monthlyFee: 200           # USD a month, split over its use on the money screens
        owner: anna@example.com
        tenant: acme              # optional: narrows the account to one tenant
        history: true             # optional, needs owner
```

The file of a real installation holds real names and addresses: it lives with the installation's secrets (OpenBao or
the deploy values), never in this repository.
