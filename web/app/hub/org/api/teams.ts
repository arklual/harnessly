// People, teams and limits of a tenant (and of one team, for its lead). The paths of the live calls are
// `GET …/scopes/{kind}/{id}/members`, `PUT …/grants`, `GET|PUT|DELETE …/limits`, `GET …/audit`, `GET|POST …/teams`
// (docs/hub/orgs.md). Money is RUB per month here (the organization's display currency). A field the viewer cannot
// change carries `needs`, the role that could: the screen disables it and says «Может поменять: …».

import type { AuditAction, AuditTargetKind } from '../../../hub-api/orgs';

export type Needs = 'org_admin' | 'tenant_admin' | 'team_lead' | 'instance_admin';
export type MemberRole = 'lead' | 'member';

export type LimitScopeKind = 'org' | 'tenant' | 'team' | 'user';
export interface LimitScope {
  kind: LimitScopeKind;
  id: string;
}

/** What a limit is measured on besides the person and the team: an account, a model. */
export type LimitAxis = 'money' | 'account' | 'model';

export interface LimitRule {
  id: string;
  axis: Exclude<LimitAxis, 'money'>;
  /** «Claude Max №2», «Opus». */
  label: string;
  amountRub: number;
  spentRub: number;
  setBy: string | null;
  canEdit: boolean;
  needs: Needs | null;
}

/** A limit that acts on a person: the one that is tightest, and who set it. */
export interface EffectiveLimit {
  amountRub: number | null;
  source: 'personal' | 'team' | 'tenant' | 'none';
  /** Whose hand it is: a name, or null for a default nobody set. */
  setBy: string | null;
  /** RFC 3339. */
  setAt: string | null;
}

export interface Person {
  id: string;
  name: string;
  role: MemberRole;
  /** Null: no team of the tenant. */
  teamId: string | null;
  /** A pending invitation: no account yet. */
  invited: boolean;
  /** RFC 3339; null: never or invited. */
  lastActiveAt: string | null;
  spentRub: number;
  /** The personal limit, null when none was set. */
  limitRub: number | null;
  limitSetBy: string | null;
  limitSetAt: string | null;
  effective: EffectiveLimit;
  chatsWeek: number | null;
  mrsWeek: number | null;
  /** How many machines the person has in this tenant; absent: the hub does not count them yet (shown as «—»). */
  machines?: number | null;
  /** The viewer themself: nobody changes their own role or limit. */
  self: boolean;
  can: { limit: boolean; role: boolean; team: boolean; remove: boolean };
  needs: { limit: Needs | null; role: Needs | null; team: Needs | null };
  rules: LimitRule[];
}

/** What the team's own page shows besides people and money, for one tenant. Every field is optional: «—» when absent. */
export interface TeamExtras {
  /** The tenants the team works in (people are shared, limits and environment are per tenant). */
  tenants?: { slug: string; name: string }[];
  /** The tenant's own name, for «Лимиты и окружение ниже — для тенанта …». */
  tenantName?: string;
  /** The tenant's limit and what it has spent: «у тенанта свободно ещё …». */
  tenantLimitRub?: number | null;
  tenantSpentRub?: number | null;
  /** The limit of machines per person (1, 2 or 3) and how many the team has now / the tenant has still free. */
  machinesPerPerson?: number | null;
  machinesNow?: number | null;
  machinesFree?: number | null;
  /** The shared accounts agents sign in with, written as the board does: «Claude Max ×2, Codex ×1». */
  accounts?: string | null;
  secrets?: number | null;
  /** What a new person gets: the number of switched-on skills, MCP, repositories and documents. */
  env?: { skills: number; mcp: number; repos: number; docs: number } | null;
}

export interface TeamBlock {
  /** Null: «Без команды». */
  id: string | null;
  name: string;
  leadName: string | null;
  spentRub: number;
  /** The team's limit; null for «без команды» (limits come from the tenant). */
  limitRub: number | null;
  limitSetBy: string | null;
  /** Sum of the personal limits as a share of the team's limit (100 = exactly the budget); null without a limit. */
  handedOutPct: number | null;
  /** The share of the tenant's limit given to the other teams. */
  siblingsLimitRub: number;
  can: { limit: boolean };
  needs: { limit: Needs | null };
  people: Person[];
  rules: LimitRule[];
  extras?: TeamExtras;
}

export interface ScopeLimit {
  label: string;
  limitRub: number | null;
  spentRub: number;
  setBy: string | null;
  can: { limit: boolean };
  needs: Needs | null;
}

export interface TeamsView {
  scope: ScopeLimit;
  teams: TeamBlock[];
  activeWeek: number;
  total: number;
  /** Seven or fourteen days of the number of people who worked, newest last. */
  activeSeries: number[];
  notAccepted: number;
  notVisiting: number;
  can: { createTeam: boolean; invite: boolean };
  /** The hub counts spend per person and team; false: the amounts are only what the limits have seen. */
  spendKnown: boolean;
}

export type { AuditAction };

/** The contract's AuditEvent (docs/hub/orgs.md), without its scope. */
export interface AuditEntry {
  id: string;
  at: string;
  actor: { userId: string; name: string } | null;
  action: AuditAction;
  target: { kind: AuditTargetKind; id: string; label: string } | null;
  before: Record<string, unknown> | null;
  after: Record<string, unknown> | null;
  self: boolean;
}

export interface AuditPage {
  events: AuditEntry[];
  next: string | null;
}

export interface TeamsApi {
  teamsView(org: string, scope: string): Promise<TeamsView>;
  /** Sets (amountRub) or removes (null) the limit of a person, a team or the tenant. Answers the new view. */
  setLimit(org: string, scope: string, on: LimitScope, amountRub: number | null): Promise<TeamsView>;
  /** Sets how many machines each person of a team may have in the tenant. Optional: a hub without it hides the row. */
  setMachinesPerPerson?(org: string, scope: string, teamId: string, n: number): Promise<TeamsView>;
  setRuleAmount(org: string, scope: string, ruleId: string, amountRub: number): Promise<TeamsView>;
  setRole(org: string, scope: string, personId: string, role: MemberRole): Promise<TeamsView>;
  moveToTeam(org: string, scope: string, personId: string, teamId: string | null): Promise<TeamsView>;
  removeFromTenant(org: string, scope: string, personId: string): Promise<TeamsView>;
  createTeam(org: string, scope: string, name: string): Promise<TeamsView>;
  teamsAudit(org: string, scope: string, before: string | null): Promise<AuditPage>;
}
