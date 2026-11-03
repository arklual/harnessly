// «Команды и лимиты» on the hub's people, grants, limits and journal (docs/hub/orgs.md, limits.md). The screen's shape is
// built by one pure function from what the hub answers (`buildTeamsView`); amounts go in and out at the answer's rate.
import type { Currency } from '../../../../hub-api/common';
import type { LimitRule as ApiRule, LimitsResponse, PutLimitRequest } from '../../../../hub-api/limits';
import type { SpendResponse } from '../../../../hub-api/analytics';
import type {
  AuditPage as ApiAuditPage,
  Member,
  Needs,
  OrgView,
  Scope as ApiScope,
  Team,
} from '../../../../hub-api/orgs';
import { setShownCurrency, spentOf } from '../../money';
import { ALL_SCOPE } from '../../types';
import { OrgApiError } from '../errors';
import type { EffectiveLimit, LimitRule, Person, ScopeLimit, TeamBlock, TeamsApi, TeamsView } from '../teams';
import { contractGet, http, orgPath } from './http';
import { capsOf, resolveScope, roleIn, viewerTeam } from './shell';

const WEEK = 7 * 24 * 3_600_000;
const NOT_VISITING = WEEK;

export interface TeamsInput {
  view: OrgView;
  scope: ApiScope;
  members: Member[];
  rules: ApiRule[];
  currency: Currency;
  /** What each person and team spent this month, when the hub serves `…/spend`; null: it does not yet. */
  spend: { byPerson: Map<string, number>; byTeam: Map<string, number>; total: number } | null;
  now: number;
}

const isMoneyMonth = (r: ApiRule): boolean =>
  r.unit === 'money' && r.period === 'month' && !r.forbid && !r.accountId && !r.modelPattern;

const sameScope = (r: ApiRule, s: ApiScope): boolean => r.scope.kind === s.kind && r.scope.id === s.id;

/** The scope's own money budget for the month. */
const budgetOf = (rules: ApiRule[], s: ApiScope): ApiRule | undefined =>
  rules.find((r) => isMoneyMonth(r) && r.userId === null && sameScope(r, s));

/** A person's limit on the axes the screen edits: in their team, else their ceiling in the tenant. */
const personalOf = (rules: ApiRule[], userId: string, team: string | null): ApiRule | undefined => {
  const own = rules.filter((r) => isMoneyMonth(r) && r.userId === userId);
  return own.find((r) => r.scope.kind === 'team' && r.scope.id === team) ?? own[0];
};

const primaryTeam = (m: Member, teams: Team[]): string | null => {
  const here = m.teams.filter((t) => teams.some((x) => x.id === t.teamId));
  return (here.find((t) => t.isPrimary) ?? here[0])?.teamId ?? null;
};

const nameOf = (m: Member): string => m.name || m.email;

export const buildTeamsView = (input: TeamsInput): TeamsView => {
  const { view, scope, members, rules, currency, spend, now } = input;
  const { me } = view;
  const shown = (usd: number): number => shownAmount(usd, currency);
  const tenantId =
    scope.kind === 'tenant'
      ? scope.id
      : scope.kind === 'team'
        ? view.teams.find((t) => t.id === scope.id)?.tenantId
        : undefined;
  const teams = view.teams.filter((t) =>
    scope.kind === 'team' ? t.id === scope.id : scope.kind === 'org' || t.tenantId === scope.id,
  );
  const tenantCaps = tenantId ? capsOf(me, { kind: 'tenant', id: tenantId }) : capsOf(me, scope);

  const uiRule = (r: ApiRule): LimitRule => {
    const caps = capsOf(me, r.scope);
    return {
      id: r.id,
      axis: r.accountId ? 'account' : 'model',
      label: r.accountId ?? r.modelPattern ?? '',
      amountRub: shown(r.amount),
      spentRub: shown(r.used),
      setBy: r.setBy?.name ?? null,
      canEdit: caps?.setLimit ?? false,
      needs: caps?.needs.setLimit ?? null,
    };
  };
  const axisRules = (keep: (r: ApiRule) => boolean): LimitRule[] =>
    rules
      .filter((r) => r.unit === 'money' && !r.forbid && (r.accountId || r.modelPattern) && keep(r))
      .map(uiRule);

  const personRow = (m: Member): Person => {
    const team = primaryTeam(m, teams);
    const self = m.userId === me.userId;
    const teamCaps = team ? capsOf(me, { kind: 'team', id: team }) : undefined;
    const isLead = m.grants.some((g) => g.role === 'lead' && g.scope.id === team);
    const limitCaps = isLead || !team ? tenantCaps : teamCaps;
    const canLimit = !self && (limitCaps?.setChildLimits ?? false);
    const limitNeeds: Needs = isLead || !team ? 'tenant_admin' : 'team_lead';
    const canRole = !self && team !== null && (teamCaps?.grant.includes('lead') ?? false);
    const canTeam = !self && (tenantCaps?.manage ?? false);
    const personal = personalOf(rules, m.userId, team);
    const budget = team ? budgetOf(rules, { kind: 'team', id: team }) : undefined;
    const tenantBudget = tenantId ? budgetOf(rules, { kind: 'tenant', id: tenantId }) : undefined;
    const binding = personal ?? budget ?? tenantBudget;
    const effective: EffectiveLimit = binding
      ? {
          amountRub: shown(binding.amount),
          source: binding.userId ? 'personal' : binding.scope.kind === 'team' ? 'team' : 'tenant',
          setBy: binding.setBy?.name ?? null,
          setAt: binding.setAt,
        }
      : { amountRub: null, source: 'none', setBy: null, setAt: null };
    return {
      id: m.userId,
      name: nameOf(m),
      role: isLead ? 'lead' : 'member',
      teamId: team,
      invited: m.invited,
      lastActiveAt: m.lastActiveAt,
      spentRub: spend?.byPerson.get(m.userId) ?? (personal ? shown(personal.used) : 0),
      limitRub: personal ? shown(personal.amount) : null,
      limitSetBy: personal?.setBy?.name ?? null,
      limitSetAt: personal?.setAt ?? null,
      effective,
      chatsWeek: null,
      mrsWeek: null,
      self,
      can: { limit: canLimit, role: canRole, team: canTeam, remove: canTeam },
      needs: {
        limit: canLimit || self ? null : limitNeeds,
        role: canRole || self ? null : 'tenant_admin',
        team: canTeam || self ? null : 'tenant_admin',
      },
      rules: axisRules((r) => r.userId === m.userId),
    };
  };

  const everyone = members.map(personRow);
  const budgets = teams.map((t) => budgetOf(rules, { kind: 'team', id: t.id }));
  const blocks: TeamBlock[] = teams.map((t, i) => {
    const people = everyone.filter((p) => p.teamId === t.id);
    const budget = budgets[i];
    const caps = capsOf(me, { kind: 'team', id: t.id });
    const handed = people.reduce((n, p) => n + (p.limitRub ?? 0), 0);
    const limit = budget ? shown(budget.amount) : null;
    return {
      id: t.id,
      name: t.name,
      leadName: everyone.find((p) => p.id === t.leadIds[0])?.name ?? null,
      spentRub:
        spend?.byTeam.get(t.id) ?? (budget ? shown(budget.used) : people.reduce((n, p) => n + p.spentRub, 0)),
      limitRub: limit,
      limitSetBy: budget?.setBy?.name ?? null,
      handedOutPct: limit ? Math.round((handed / limit) * 100) : null,
      siblingsLimitRub: budgets.reduce((n, b, j) => n + (b && j !== i ? shown(b.amount) : 0), 0),
      can: { limit: caps?.setLimit ?? false },
      needs: { limit: caps?.needs.setLimit ?? null },
      people,
      rules: axisRules((r) => r.userId === null && r.scope.kind === 'team' && r.scope.id === t.id),
    };
  });
  const loose = everyone.filter((p) => p.teamId === null);
  if (scope.kind !== 'team' && loose.length > 0) {
    blocks.push({
      id: null,
      name: 'Без команды',
      leadName: null,
      spentRub: loose.reduce((n, p) => n + p.spentRub, 0),
      limitRub: null,
      limitSetBy: null,
      handedOutPct: null,
      siblingsLimitRub: 0,
      can: { limit: false },
      needs: { limit: null },
      people: loose,
      rules: [],
    });
  }

  const own = budgetOf(rules, scope);
  const caps = capsOf(me, scope);
  const tenant = view.tenants.find((t) => t.id === tenantId);
  const team = scope.kind === 'team' ? teams[0] : undefined;
  const label = team
    ? `Команда ${team.name}`
    : tenant
      ? `Тенант ${tenant.name}`
      : `Организация ${view.org.name}`;
  const scopeLimit: ScopeLimit = {
    label,
    limitRub: own ? shown(own.amount) : null,
    spentRub: spend?.total ?? (own ? shown(own.used) : everyone.reduce((n, p) => n + p.spentRub, 0)),
    setBy: own?.setBy?.name ?? null,
    can: { limit: caps?.setLimit ?? false },
    needs: caps?.needs.setLimit ?? null,
  };
  const seenWithin = (p: Person, ms: number) =>
    p.lastActiveAt !== null && now - Date.parse(p.lastActiveAt) < ms;
  return {
    scope: scopeLimit,
    teams: blocks,
    activeWeek: everyone.filter((p) => seenWithin(p, WEEK)).length,
    total: everyone.length,
    activeSeries: [],
    notAccepted: everyone.filter((p) => p.invited).length,
    notVisiting: everyone.filter((p) => !p.invited && !seenWithin(p, NOT_VISITING)).length,
    can: { createTeam: scope.kind === 'tenant' && (caps?.manage ?? false), invite: caps?.manage ?? false },
    spendKnown: spend !== null,
  };
};

/** Where a person's limit lives: in their team, else in the tenant. */
export const personalScope = (input: TeamsInput, userId: string): ApiScope => {
  const member = input.members.find((m) => m.userId === userId);
  if (!member) throw new OrgApiError('not_found', 'person not found');
  const team = primaryTeam(member, input.view.teams);
  if (team) return { kind: 'team', id: team };
  const tenant =
    input.scope.kind === 'tenant'
      ? input.scope.id
      : input.view.tenants.length === 1
        ? input.view.tenants[0]?.id
        : undefined;
  if (!tenant)
    throw new OrgApiError('invalid_request', 'Откройте тенант этого человека, чтобы поставить лимит.');
  return { kind: 'tenant', id: tenant };
};

/** An amount in the shown currency. Rubles are whole: 372,40 $ at 80,5573 is the 30 000 ₽ someone typed, not 30 000,05. */
export const shownAmount = (usd: number, currency: Currency): number =>
  currency.code === 'RUB' ? Math.round(usd * currency.perUsd) : Math.round(usd * 100) / 100;

/** A limit typed in the shown currency, as the hub keeps it: dollars. */
export const toUsd = (shown: number, currency: Currency): number =>
  Math.round((shown / currency.perUsd) * 100) / 100;

const moneyRule = (
  scope: ApiScope,
  userId: string | null,
  shown: number,
  currency: Currency,
): PutLimitRequest => ({
  scope,
  userId,
  accountId: null,
  modelPattern: null,
  forbid: false,
  period: 'month',
  unit: 'money',
  amount: toUsd(shown, currency),
});

const spendOf = (r: SpendResponse) => new Map(r.rows.map((x) => [x.key, spentOf(x).shown]));

export const liveTeams = (): TeamsApi => {
  let rate = 1;
  const scopeOfViewer = (view: OrgView, slug: string): ApiScope => {
    const base = resolveScope(view, slug);
    if (roleIn(view, base) !== 'lead') return base;
    const team = viewerTeam(view, base);
    return team ? { kind: 'team', id: team } : base;
  };

  const read = async (org: string, slug: string): Promise<TeamsInput> => {
    const view = await http.get<OrgView>(orgPath(org));
    const scope = scopeOfViewer(view, slug);
    const key = `${scope.kind}:${scope.id}`;
    const inside = view.teams.filter((t) => scope.kind === 'org' || t.tenantId === scope.id);
    const childScopes: ApiScope[] =
      scope.kind === 'team' ? [] : inside.map((t) => ({ kind: 'team', id: t.id }));
    const [{ members }, limits, ...children] = await Promise.all([
      http.get<{ members: Member[] }>(orgPath(org, `/scopes/${scope.kind}/${scope.id}/members`)),
      http.get<LimitsResponse>(orgPath(org, '/limits'), { scope: key }),
      ...childScopes.map((c) =>
        http.get<LimitsResponse>(orgPath(org, '/limits'), { scope: `${c.kind}:${c.id}` }),
      ),
    ]);
    const byId = new Map([limits, ...children].flatMap((r) => r.rules).map((r) => [r.id, r]));
    setShownCurrency(limits.currency.code);
    rate = limits.currency.perUsd;
    const spend = await readSpend(org, key);
    return {
      view,
      scope,
      members,
      rules: [...byId.values()],
      currency: limits.currency,
      spend,
      now: Date.now(),
    };
  };

  const readSpend = async (org: string, scope: string): Promise<TeamsInput['spend']> => {
    try {
      const [person, team] = await Promise.all([
        contractGet<SpendResponse>(orgPath(org, '/spend'), { scope, by: 'person', period: 'month' }),
        contractGet<SpendResponse>(orgPath(org, '/spend'), { scope, by: 'team', period: 'month' }),
      ]);
      return { byPerson: spendOf(person), byTeam: spendOf(team), total: spentOf(person.total).shown };
    } catch (e) {
      if (e instanceof OrgApiError && e.code === 'not_ready') return null;
      throw e;
    }
  };

  const load = async (org: string, slug: string) => buildTeamsView(await read(org, slug));

  return {
    teamsView: load,
    setLimit: async (org, slug, on, amount) => {
      const input = await read(org, slug);
      const person = on.kind === 'user' ? on.id : null;
      const target: ApiScope =
        on.kind === 'user' ? personalScope(input, on.id) : { kind: on.kind, id: on.id };
      if (amount === null) {
        const rule = input.rules.find((r) => isMoneyMonth(r) && r.userId === person && sameScope(r, target));
        if (rule) await http.del(orgPath(org, `/limits/${rule.id}`));
      } else {
        await http.put(orgPath(org, '/limits'), moneyRule(target, person, amount, input.currency));
      }
      return load(org, slug);
    },
    setRuleAmount: async (org, slug, ruleId, amount) => {
      const input = await read(org, slug);
      const rule = input.rules.find((r) => r.id === ruleId);
      if (!rule) throw new OrgApiError('not_found', 'rule not found');
      const body: PutLimitRequest = {
        scope: rule.scope,
        userId: rule.userId,
        accountId: rule.accountId,
        modelPattern: rule.modelPattern,
        forbid: rule.forbid,
        period: rule.period,
        unit: rule.unit,
        amount: toUsd(amount, input.currency),
        warnPct: rule.warnPct,
      };
      await http.put(orgPath(org, '/limits'), body);
      return load(org, slug);
    },
    setRole: async (org, slug, personId, role) => {
      const input = await read(org, slug);
      const scope = personalScope(input, personId);
      if (scope.kind !== 'team') {
        throw new OrgApiError('invalid_request', 'Лидом можно сделать только человека из команды.');
      }
      await http.put(orgPath(org, '/grants'), {
        userId: personId,
        scope,
        role: 'lead',
        granted: role === 'lead',
      });
      return load(org, slug);
    },
    moveToTeam: async (org, slug, personId, teamId) => {
      const input = await read(org, slug);
      const member = input.members.find((m) => m.userId === personId);
      if (!member) throw new OrgApiError('not_found', 'person not found');
      const from = primaryTeam(member, input.view.teams);
      if (teamId) {
        await http.put(orgPath(org, `/teams/${teamId}/members/${personId}`), { isPrimary: true });
      }
      if (from && from !== teamId) await http.del(orgPath(org, `/teams/${from}/members/${personId}`));
      return load(org, slug);
    },
    removeFromTenant: async (org, slug, personId) => {
      const input = await read(org, slug);
      if (input.scope.kind === 'team') {
        await http.del(orgPath(org, `/teams/${input.scope.id}/members/${personId}`));
      } else if (input.scope.kind === 'tenant') {
        await http.del(`/api/spaces/${input.scope.id}/members/${personId}`);
      } else {
        throw new OrgApiError('invalid_request', 'Откройте тенант этого человека, чтобы убрать его.');
      }
      return load(org, slug);
    },
    createTeam: async (org, slug, name) => {
      const input = await read(org, slug);
      if (input.scope.kind !== 'tenant') {
        throw new OrgApiError('invalid_request', 'Откройте тенант, чтобы создать в нём команду.');
      }
      await http.post(orgPath(org, '/teams'), { tenantId: input.scope.id, name: name.trim() });
      return load(org, slug);
    },
    teamsAudit: async (org, slug, before) => {
      const view = await http.get<OrgView>(orgPath(org));
      const scope = slug === ALL_SCOPE ? resolveScope(view, slug) : scopeOfViewer(view, slug);
      const page = await http.get<ApiAuditPage>(orgPath(org, '/audit'), {
        scope: `${scope.kind}:${scope.id}`,
        before: before ?? undefined,
        limit: 50,
      });
      // The journal keeps a limit as the rule; the screen reads the amount in the shown currency.
      const amount = (v: Record<string, unknown> | null) =>
        v && typeof v.amount === 'number' ? { ...v, limitRub: v.amount * rate } : v;
      return {
        next: page.next,
        events: page.events.map(({ scope: _scope, ...e }) => ({
          ...e,
          before: amount(e.before),
          after: amount(e.after),
        })),
      };
    },
  };
};
