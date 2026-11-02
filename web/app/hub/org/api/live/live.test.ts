import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

import type {
  EffectResponse,
  HubMachine,
  KpisResponse,
  MeSessionsResponse,
  MeUsageResponse,
  OverviewResponse,
} from '../../../../hub-api/analytics';
import type { Currency } from '../../../../hub-api/common';
import type { LimitRule, MeLimitsResponse } from '../../../../hub-api/limits';
import type { Member, OrgView } from '../../../../hub-api/orgs';
import { spentOf } from '../../money';
import { accessBody, accessOf, buildMachinesList } from './machines';
import { changeOf, effectsOf, kpiOf, peersOf, peerScore, weeksUpTo } from './analytics';
import { buildMyOverview, forecastOf } from './member';
import { buildOverview } from './overview';
import { resolveScope, roleIn, viewerTeam } from './shell';
import { buildTeamsView, personalScope, shownAmount, toUsd, type TeamsInput } from './teams';

const fixture = (name: string): unknown =>
  JSON.parse(readFileSync(new URL(`../../../../hub-api/fixtures/${name}.json`, import.meta.url), 'utf8'));
const kpisAdmin = fixture('analytics/kpis-admin');
const sessionsMember = fixture('analytics/me-sessions-member');
const usageMember = fixture('analytics/me-usage-member');
const overviewAdmin = fixture('analytics/overview-admin');
const overviewLead = fixture('analytics/overview-lead');
const viewAdmin = fixture('orgs/view-admin');
const viewLead = fixture('orgs/view-lead');
const viewMember = fixture('orgs/view-member');
const members = fixture('orgs/members');

const admin = viewAdmin as unknown as OrgView;
const lead = viewLead as unknown as OrgView;
const member = viewMember as unknown as OrgView;
const people = (members as unknown as { members: Member[] }).members;
const USD: Currency = { code: 'USD', perUsd: 1, rateDate: '2026-11-01', source: 'none' };
const RUB: Currency = { code: 'RUB', perUsd: 90, rateDate: '2026-11-01', source: 'cbr' };

const rule = (over: Partial<LimitRule>): LimitRule => ({
  id: 'r1',
  scope: { kind: 'tenant', id: 'x' },
  userId: null,
  accountId: null,
  modelPattern: null,
  forbid: false,
  period: 'month',
  unit: 'money',
  amount: 100,
  warnPct: 0.8,
  used: 25,
  pct: 0.25,
  status: 'ok',
  resetsAt: '2026-12-01T00:00:00Z',
  estimated: false,
  setBy: { userId: 'u1', name: 'Анна' },
  setAt: '2026-11-01T00:00:00Z',
  ...over,
});

describe('who is looking', () => {
  it('tells the role from the scope, not from the organization', () => {
    const tenant = admin.tenants[0];
    if (!tenant) throw new Error('fixture has no tenant');
    expect(roleIn(admin, { kind: 'org', id: admin.org.id })).toBe('admin');
    expect(roleIn(admin, { kind: 'tenant', id: tenant.id })).toBe('admin');
    const leadTenant = lead.tenants[0];
    if (!leadTenant) throw new Error('fixture has no tenant');
    expect(roleIn(lead, { kind: 'tenant', id: leadTenant.id })).toBe('lead');
    expect(viewerTeam(lead, { kind: 'tenant', id: leadTenant.id })).not.toBeNull();
    const memberTenant = member.tenants[0];
    if (!memberTenant) throw new Error('fixture has no tenant');
    expect(roleIn(member, { kind: 'tenant', id: memberTenant.id })).toBe('member');
  });

  it('says a tenant of another address is not there', () => {
    expect(() => resolveScope(admin, 'no-such-tenant')).toThrow();
    expect(resolveScope(admin, 'all')).toEqual({ kind: 'org', id: admin.org.id });
  });
});

describe('a limit keeps its money', () => {
  const input = (currency: Currency, rules: LimitRule[]): TeamsInput => {
    const tenant = admin.tenants[0];
    if (!tenant) throw new Error('fixture has no tenant');
    return {
      view: admin,
      scope: { kind: 'tenant', id: tenant.id },
      members: people,
      rules,
      currency,
      spend: null,
      now: Date.parse('2026-11-02T12:00:00Z'),
    };
  };

  it('never turns a typed amount into dollars at a made-up rate', () => {
    expect(toUsd(30000, USD)).toBe(30000);
    expect(toUsd(9000, RUB)).toBe(100);
  });

  it('brings a typed amount back as it was typed, at a rate with a long tail', () => {
    const rate: Currency = { code: 'RUB', perUsd: 80.5573, rateDate: '2026-11-01', source: 'cbr' };
    for (const typed of [5000, 15000, 30000, 80000, 300000]) {
      expect(shownAmount(toUsd(typed, rate), rate)).toBe(typed);
    }
  });

  it('shows the budget at the rate of the answer', () => {
    const tenant = admin.tenants[0];
    if (!tenant) throw new Error('fixture has no tenant');
    const scope = { kind: 'tenant', id: tenant.id } as const;
    expect(buildTeamsView(input(USD, [rule({ scope })])).scope.limitRub).toBe(100);
    expect(buildTeamsView(input(RUB, [rule({ scope })])).scope.limitRub).toBe(9000);
  });

  it('marks the spend as unknown while the hub has no …/spend', () => {
    expect(buildTeamsView(input(USD, [])).spendKnown).toBe(false);
  });

  it('nobody edits their own limit or role', () => {
    const view = buildTeamsView(input(USD, []));
    const self = view.teams.flatMap((t) => t.people).find((p) => p.self);
    if (!self) throw new Error('the fixture viewer is not among the people');
    expect(self.can).toEqual({ limit: false, role: false, team: false, remove: false });
    expect(self.needs).toEqual({ limit: null, role: null, team: null });
  });

  it('every person lands in exactly one block', () => {
    const view = buildTeamsView(input(USD, []));
    const shown = view.teams.flatMap((t) => t.people).map((p) => p.id);
    expect(new Set(shown).size).toBe(shown.length);
    expect(shown.length).toBe(people.length);
  });

  it('a personal limit goes to the person’s team, else to the tenant', () => {
    const i = input(USD, []);
    const inTeam = people.find((m) => m.teams.length > 0);
    if (!inTeam) throw new Error('fixture has nobody in a team');
    expect(personalScope(i, inTeam.userId).kind).toBe('team');
    expect(() => personalScope(i, 'nobody')).toThrow();
  });
});

describe('the participant’s view', () => {
  const limits = (rules: LimitRule[]): MeLimitsResponse => ({
    mode: 'off',
    currency: USD,
    rules,
    effective: [],
    machines: [],
  });
  const usage = usageMember as unknown as MeUsageResponse;
  const { sessions } = sessionsMember as unknown as MeSessionsResponse;
  const now = Date.parse('2026-11-02T12:00:00Z');

  it('has no limit card without a rule', () => {
    expect(buildMyOverview({ limits: limits([]), usage, sessions, now }).limit).toBeNull();
  });

  it('takes the money rule closest to its end, not an account’s window', () => {
    const data = buildMyOverview({
      limits: limits([
        rule({ id: 'low', pct: 0.1, amount: 500, used: 50 }),
        rule({ id: 'high', pct: 0.84, amount: 100, used: 84 }),
        rule({ id: 'window', pct: 1.2, unit: 'window_pct', amount: 90, used: 108 }),
      ]),
      usage,
      sessions,
      now,
    });
    expect(data.limit?.limitRub).toBe(100);
    expect(data.limit?.spentRub).toBe(84);
    expect(data.limit?.warnPct).toBe(80);
  });

  it('takes the personal limit over the team’s budget while nothing is spent', () => {
    const data = buildMyOverview({
      limits: limits([
        rule({ id: 'team', pct: 0, amount: 500, used: 0 }),
        rule({ id: 'mine', pct: 0, amount: 50, used: 0, userId: 'u2' }),
      ]),
      usage,
      sessions,
      now,
    });
    expect(data.limit?.limitRub).toBe(50);
  });

  it('says nothing of merged MRs the hub does not tell', () => {
    expect(buildMyOverview({ limits: limits([]), usage, sessions, now }).tiles.mr.merged).toBeNull();
  });

  it('projects the month from the pace so far', () => {
    expect(forecastOf(150, Date.parse('2026-11-16T00:00:00Z'))).toBe(300);
  });
});

describe('analytics', () => {
  it('takes the unit and the verdict from the hub', () => {
    const r = kpisAdmin as unknown as KpisResponse;
    const kpis = r.kpis.map(kpiOf);
    expect(kpis).toHaveLength(12);
    const source = r.kpis.find((k) => k.kpi === 'completion');
    const mapped = kpis.find((k) => k.id === 'yield');
    if (!source?.value || !mapped) throw new Error('fixture has no completion');
    expect(mapped.value).toBeCloseTo(source.value * 100);
    for (const [i, k] of r.kpis.entries()) {
      if (k.verdict === 'noise' || k.verdict === 'too_few') expect(kpis[i]?.noise).toBe(Infinity);
    }
  });

  it('keeps a value of too few sessions empty, not zero', () => {
    const k = kpiOf({
      kpi: 'cache_hit',
      unit: 'share',
      betterWhen: 'up',
      value: null,
      n: 2,
      cmpValue: null,
      cmpN: 0,
      delta: null,
      noise: null,
      verdict: 'too_few',
    });
    expect(k.value).toBeNull();
  });

  it('sets teams next to each other only when there are two to compare', () => {
    const r = kpisAdmin as unknown as KpisResponse;
    const one = r.kpis;
    expect(peersOf({ ...r, series: null })).toBeNull();
    expect(peersOf({ ...r, series: [{ key: 'a', label: 'А', kpis: one }] })).toBeNull();
    const peers = peersOf({
      ...r,
      series: [
        { key: 'a', label: 'А', kpis: one },
        { key: 'b', label: 'Б', kpis: [] },
      ],
    });
    expect(peers?.rows).toHaveLength(2);
    expect(peers?.columns).toHaveLength(8);
    expect(peers?.rows[1]?.cells.every((c) => c.text === '—' && c.score === 0)).toBe(true);
  });

  it('scores a team by which way is better for that KPI', () => {
    const v = (value: number | null, betterWhen: 'up' | 'down') =>
      ({
        kpi: 'cache_hit',
        unit: 'share',
        betterWhen,
        value,
        n: 9,
        cmpValue: null,
        cmpN: 0,
        delta: null,
        noise: null,
        verdict: 'better',
      }) as const;
    expect(peerScore(v(0.6, 'up'), v(0.5, 'up'))).toBeCloseTo(1);
    expect(peerScore(v(0.6, 'down'), v(0.5, 'down'))).toBeCloseTo(-1);
    expect(peerScore(v(null, 'up'), v(0.5, 'up'))).toBe(0);
  });

  it('offers whole weeks ending with the current one', () => {
    const weeks = weeksUpTo(new Date('2026-11-04T12:00:00Z'));
    expect(weeks[weeks.length - 1]).toEqual({ from: '2026-11-02', to: '2026-11-08' });
  });
});

describe('overview', () => {
  it('gives an admin teams and a lead people', () => {
    const a = buildOverview(overviewAdmin as unknown as OverviewResponse, null, 10);
    expect(a.byTeam).not.toBeNull();
    expect(a.byPerson).toBeNull();
    const l = buildOverview(overviewLead as unknown as OverviewResponse, 'Платформа', 5);
    expect(l.byPerson?.teamName).toBe('Платформа');
    expect(l.byTeam).toBeNull();
  });
});

describe('machines', () => {
  const base: HubMachine = {
    id: 'm1',
    name: 'build',
    hostname: 'build-1',
    tenantId: admin.tenants[0]?.id ?? '',
    teamId: null,
    status: 'online',
    disabledAt: null,
    agentVersion: '1.0.0',
    lastSeenAt: '2026-11-02T00:00:00Z',
    lastActiveAt: '2026-11-02T00:00:00Z',
    chatsRunning: 2,
    people: [],
    access: [],
    problems: [],
    cloud: null,
  };
  const ctx = { org: 'o', view: admin, scope: { kind: 'org', id: admin.org.id } as const };

  it('counts each machine by its state, and says nothing of chats it does not count', () => {
    const list = buildMachinesList(
      [
        base,
        { ...base, id: 'm2', status: 'offline' },
        { ...base, id: 'm3', problems: ['idle_week'] },
        { ...base, id: 'm4', disabledAt: '2026-11-01T00:00:00Z', problems: ['disabled'] },
        { ...base, id: 'm5', problems: ['agent_old'] },
      ],
      ctx,
    );
    expect(list.summary).toMatchObject({ total: 5, online: 2, offline: 1, idleWeek: 1, outdatedRunners: 1 });
    expect(list.summary.chatsToday).toBeNull();
    expect(list.groups[0]?.machines.find((m) => m.id === 'm4')?.state).toBe('disabled');
  });

  it('reads and writes access: empty is the whole tenant', () => {
    expect(accessOf(base).mode).toBe('tenant');
    expect(accessOf({ ...base, teamId: 't', access: [{ kind: 'team', id: 't', label: 'Т' }] }).mode).toBe(
      'team',
    );
    expect(accessOf({ ...base, access: [{ kind: 'user', id: 'u', label: 'У' }] }).people).toEqual([
      { id: 'u', name: 'У' },
    ]);
    expect(accessBody(base, { mode: 'tenant', personIds: [] })).toEqual([]);
    expect(accessBody(base, { mode: 'pick', personIds: ['u'] })).toEqual([{ kind: 'user', id: 'u' }]);
    expect(() => accessBody(base, { mode: 'team', personIds: [] })).toThrow();
  });
});

describe('a change and its effect', () => {
  const scope = { kind: 'team', id: 't' } as const;
  const effect = (over: Partial<EffectResponse['effects'][number]>) => ({
    kpi: 'completion' as const,
    scope,
    before: 0.5,
    after: 0.6,
    delta: 0.1,
    noise: 0.01,
    nBefore: 9,
    nAfter: 9,
    verdict: 'better' as const,
    ...over,
  });
  const response = (effects: EffectResponse['effects']): EffectResponse => ({
    change: {
      id: 'c',
      scope,
      kind: 'default_model',
      title: 'Sonnet',
      at: '2026-10-09T00:00:00Z',
      author: null,
      source: 'detected',
      machines: 3,
    },
    before: { from: '2026-10-02', to: '2026-10-09', tz: 'UTC' },
    after: { from: '2026-10-09', to: '2026-10-16', tz: 'UTC' },
    overlaps: [],
    effects,
  });

  it('leaves out a KPI of too few sessions, shares in points', () => {
    const shown = effectsOf(
      response([effect({}), effect({ kpi: 'cache_hit', before: null, verdict: 'too_few' })]),
    );
    expect(shown).toHaveLength(1);
    expect(shown[0]).toMatchObject({ kpi: 'yield', before: 50, after: 60, verdict: 'better' });
  });

  it('shows what moved rather than the noise, and a mix as noise when nothing did', () => {
    const shown = effectsOf(
      response([effect({ kpi: 'cache_hit', verdict: 'noise' }), effect({ verdict: 'worse' })]),
    );
    expect(shown.map((e) => e.verdict)).toEqual(['worse']);
    expect(effectsOf(response([effect({ verdict: 'mixed' })]))[0]?.verdict).toBe('noise');
  });

  it('names who made it, or that the hub noticed', () => {
    const r = response([]);
    expect(changeOf(r.change, null)).toMatchObject({
      kind: 'model',
      at: '2026-10-09',
      meta: 'хаб заметил сам',
      effects: [],
    });
  });
});

describe('spend of an account with no price', () => {
  const m = (usd: number) => ({ usd, shown: usd * 80 });

  it('is the list amount and says it is an estimate', () => {
    expect(spentOf({ list: m(10), paid: m(0) })).toEqual({ shown: 800, usd: 10, estimated: true });
  });

  it('is what was paid when something was', () => {
    expect(spentOf({ list: m(10), paid: m(4) })).toEqual({ shown: 320, usd: 4, estimated: false });
  });

  it('is no estimate when nothing was spent', () => {
    expect(spentOf({ list: m(0), paid: m(0) }).estimated).toBe(false);
  });
});
