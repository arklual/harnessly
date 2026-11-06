// The contracts of docs/hub/{orgs,usage,analytics,limits,environment,machines-resources}.md: every schema infers exactly its TS type (checked by tsc),
// and every mock fixture passes its schema (checked here).

import { describe, expect, it } from 'vitest';

import type * as A from './analytics';
import * as as_ from './analytics.schema';
import type * as C from './common';
import type * as E from './env';
import * as es from './env.schema';
import * as cs from './common.schema';
import type * as L from './limits';
import * as ls from './limits.schema';
import type * as M from './machines';
import * as ms from './machines.schema';
import type * as O from './orgs';
import * as os from './orgs.schema';
import { assertType, type Equal, type Infer, type Schema } from './schema';

assertType<Equal<Infer<typeof os.org>, O.Org>>(true);
assertType<Equal<Infer<typeof os.tenant>, O.Tenant>>(true);
assertType<Equal<Infer<typeof os.team>, O.Team>>(true);
assertType<Equal<Infer<typeof os.grant>, O.Grant>>(true);
assertType<Equal<Infer<typeof os.capabilities>, O.Capabilities>>(true);
assertType<Equal<Infer<typeof os.me>, O.Me>>(true);
assertType<Equal<Infer<typeof os.orgView>, O.OrgView>>(true);
assertType<Equal<Infer<typeof os.featureView>, O.FeatureView>>(true);
assertType<Equal<Infer<typeof os.featuresResponse>, O.FeaturesResponse>>(true);
assertType<Equal<Infer<typeof os.orgList>, O.OrgList>>(true);
assertType<Equal<Infer<typeof os.member>, O.Member>>(true);
assertType<Equal<Infer<typeof os.auditEvent>, O.AuditEvent>>(true);
assertType<Equal<Infer<typeof os.auditPage>, O.AuditPage>>(true);

assertType<Equal<Infer<typeof cs.money>, C.Money>>(true);
assertType<Equal<Infer<typeof cs.currency>, C.Currency>>(true);
assertType<Equal<Infer<typeof cs.period>, C.Period>>(true);
assertType<Equal<Infer<typeof as_.kpiValue>, A.KpiValue>>(true);
assertType<Equal<Infer<typeof as_.kpisResponse>, A.KpisResponse>>(true);
assertType<Equal<Infer<typeof as_.kpiSeriesResponse>, A.KpiSeriesResponse>>(true);
assertType<Equal<Infer<typeof as_.signalAction>, A.SignalAction>>(true);
assertType<Equal<Infer<typeof as_.signal>, A.Signal>>(true);
assertType<Equal<Infer<typeof as_.signalsResponse>, A.SignalsResponse>>(true);
assertType<Equal<Infer<typeof as_.limitBar>, A.LimitBar>>(true);
assertType<Equal<Infer<typeof as_.spendRow>, A.SpendRow>>(true);
assertType<Equal<Infer<typeof as_.spendResponse>, A.SpendResponse>>(true);
assertType<Equal<Infer<typeof as_.windowView>, A.WindowView>>(true);
assertType<Equal<Infer<typeof as_.subscription>, A.Subscription>>(true);
assertType<Equal<Infer<typeof as_.subscriptionsResponse>, A.SubscriptionsResponse>>(true);
assertType<Equal<Infer<typeof as_.overviewResponse>, A.OverviewResponse>>(true);
assertType<Equal<Infer<typeof as_.change>, A.Change>>(true);
assertType<Equal<Infer<typeof as_.changesResponse>, A.ChangesResponse>>(true);
assertType<Equal<Infer<typeof as_.effectResponse>, A.EffectResponse>>(true);
assertType<Equal<Infer<typeof as_.wasteResponse>, A.WasteResponse>>(true);
assertType<Equal<Infer<typeof as_.peopleResponse>, A.PeopleResponse>>(true);
assertType<Equal<Infer<typeof as_.meWeek>, A.MeWeek>>(true);
assertType<Equal<Infer<typeof as_.hubMachine>, A.HubMachine>>(true);
assertType<Equal<Infer<typeof as_.machinesResponse>, A.MachinesResponse>>(true);
assertType<Equal<Infer<typeof as_.restartRequest>, A.RestartRequest>>(true);
assertType<Equal<Infer<typeof as_.restartResponse>, A.RestartResponse>>(true);
assertType<Equal<Infer<typeof as_.meUsageResponse>, A.MeUsageResponse>>(true);
assertType<Equal<Infer<typeof as_.meSessionsResponse>, A.MeSessionsResponse>>(true);
assertType<Equal<Infer<typeof as_.limitRequestResponse>, A.LimitRequestResponse>>(true);

assertType<Equal<Infer<typeof ms.machinePrice>, M.MachinePrice>>(true);
assertType<Equal<Infer<typeof ms.machineSize>, M.MachineSize>>(true);
assertType<Equal<Infer<typeof ms.machinePlan>, M.MachinePlan>>(true);
assertType<Equal<Infer<typeof ms.machineHardware>, M.MachineHardware>>(true);
assertType<Equal<Infer<typeof ms.machineCloud>, M.MachineCloud>>(true);
assertType<Equal<Infer<typeof ms.machineLoad>, M.MachineLoad>>(true);
assertType<Equal<Infer<typeof ms.machineWeek>, M.MachineWeek>>(true);
assertType<Equal<Infer<typeof ms.machineManaged>, M.MachineManaged>>(true);
assertType<Equal<Infer<typeof ms.managedCapability>, M.ManagedCapability>>(true);
assertType<Equal<Infer<typeof ms.machineOp>, M.MachineOp>>(true);
assertType<Equal<Infer<typeof ms.resourcesResponse>, M.ResourcesResponse>>(true);
assertType<Equal<Infer<typeof ms.sizeRequest>, M.SizeRequest>>(true);
assertType<Equal<Infer<typeof ms.quoteResponse>, M.QuoteResponse>>(true);
assertType<Equal<Infer<typeof ms.resizeResponse>, M.ResizeResponse>>(true);
assertType<Equal<Infer<typeof ms.opsResponse>, M.OpsResponse>>(true);
assertType<Equal<Infer<typeof ms.opResponse>, M.OpResponse>>(true);
assertType<Equal<Infer<typeof ms.managedRequest>, M.ManagedRequest>>(true);
assertType<Equal<Infer<typeof ms.managedResponse>, M.ManagedResponse>>(true);
assertType<Equal<Infer<typeof ms.metricPoint>, M.MetricPoint>>(true);
assertType<Equal<Infer<typeof ms.metricsResponse>, M.MetricsResponse>>(true);
assertType<Equal<Infer<typeof ms.machineOpEvent>, M.MachineOpEvent>>(true);

assertType<Equal<Infer<typeof ls.limitRule>, L.LimitRule>>(true);
assertType<Equal<Infer<typeof ls.effectiveLimit>, L.EffectiveLimit>>(true);
assertType<Equal<Infer<typeof ls.personLimits>, L.PersonLimits>>(true);
assertType<Equal<Infer<typeof ls.limitsResponse>, L.LimitsResponse>>(true);
assertType<Equal<Infer<typeof ls.putLimitRequest>, L.PutLimitRequest>>(true);
assertType<Equal<Infer<typeof ls.putLimitResponse>, L.PutLimitResponse>>(true);
assertType<Equal<Infer<typeof ls.machineBudget>, L.MachineBudget>>(true);
assertType<Equal<Infer<typeof ls.meLimitsResponse>, L.MeLimitsResponse>>(true);

assertType<Equal<Infer<typeof es.envSummary>, E.EnvSummary>>(true);
assertType<Equal<Infer<typeof es.envEntryRow>, E.EnvEntryRow>>(true);
assertType<Equal<Infer<typeof es.envLayer>, E.EnvLayer>>(true);
assertType<Equal<Infer<typeof es.teamRef>, E.TeamRef>>(true);
assertType<Equal<Infer<typeof es.envResponse>, E.EnvResponse>>(true);
assertType<Equal<Infer<typeof es.envCard>, E.EnvCard>>(true);
assertType<Equal<Infer<typeof es.catalogItem>, E.CatalogItem>>(true);
assertType<Equal<Infer<typeof es.catalogResponse>, E.CatalogResponse>>(true);
assertType<Equal<Infer<typeof es.catalogItemResponse>, E.CatalogItemResponse>>(true);
assertType<Equal<Infer<typeof es.meEnvItem>, E.MeEnvItem>>(true);
assertType<Equal<Infer<typeof es.meEnvResponse>, E.MeEnvResponse>>(true);
assertType<Equal<Infer<typeof es.envRunStep>, E.EnvRunStep>>(true);
assertType<Equal<Infer<typeof es.envRunRow>, E.EnvRunRow>>(true);
assertType<Equal<Infer<typeof es.envRunsResponse>, E.EnvRunsResponse>>(true);
assertType<Equal<Infer<typeof es.envRunResponse>, E.EnvRunResponse>>(true);
assertType<Equal<Infer<typeof es.envTryResponse>, E.EnvTryResponse>>(true);
assertType<Equal<Infer<typeof es.envOrderResponse>, E.EnvOrderResponse>>(true);
assertType<Equal<Infer<typeof es.meMachine>, E.MeMachine>>(true);
assertType<Equal<Infer<typeof es.meMachinesResponse>, E.MeMachinesResponse>>(true);
assertType<Equal<Infer<typeof es.meRunResponse>, E.MeRunResponse>>(true);
assertType<Equal<Infer<typeof es.meRunsResponse>, E.MeRunsResponse>>(true);

const fixtures = import.meta.glob<unknown>('./fixtures/**/*.json', { eager: true, import: 'default' });

// Which schema each fixture file is checked against, by its path under fixtures/.
const schemaOf: Record<string, Schema<unknown>> = {
  'orgs/list.json': os.orgList,
  'orgs/view-admin.json': os.orgView,
  'orgs/view-lead.json': os.orgView,
  'orgs/view-member.json': os.orgView,
  'orgs/features.json': os.featuresResponse,
  'orgs/members.json': os.members,
  'orgs/audit.json': os.auditPage,
  'analytics/overview-admin.json': as_.overviewResponse,
  'analytics/overview-lead.json': as_.overviewResponse,
  'analytics/signals-lead.json': as_.signalsResponse,
  'analytics/signals-admin.json': as_.signalsResponse,
  'analytics/spend-admin-model.json': as_.spendResponse,
  'analytics/subscriptions-admin.json': as_.subscriptionsResponse,
  'analytics/kpis-admin.json': as_.kpisResponse,
  'analytics/kpi-series.json': as_.kpiSeriesResponse,
  'analytics/me-kpi-series-member.json': as_.kpiSeriesResponse,
  'analytics/people-lead.json': as_.peopleResponse,
  'analytics/me-week-member.json': as_.meWeek,
  'analytics/changes.json': as_.changesResponse,
  'analytics/effect.json': as_.effectResponse,
  'analytics/waste-admin.json': as_.wasteResponse,
  'analytics/machines-admin.json': as_.machinesResponse,
  'analytics/me-usage-member.json': as_.meUsageResponse,
  'analytics/me-sessions-member.json': as_.meSessionsResponse,
  'analytics/me-kpis-member.json': as_.kpisResponse,
  'analytics/limit-request.json': as_.limitRequestResponse,
  'analytics/restart.json': as_.restartResponse,
  'machines/resources.json': ms.resourcesResponse,
  'machines/resources-no-provider.json': ms.resourcesResponse,
  'machines/quote.json': ms.quoteResponse,
  'machines/resize.json': ms.resizeResponse,
  'machines/ops.json': ms.opsResponse,
  'machines/managed.json': ms.managedResponse,
  'machines/metrics.json': ms.metricsResponse,
  'machines/op-event.json': ms.machineOpEvent,
  'limits/vera.json': ls.meLimitsResponse,
  'limits/team-lead.json': ls.limitsResponse,
  'limits/put.json': ls.putLimitResponse,
  'env/env-admin.json': es.envResponse,
  'env/card.json': es.envCard,
  'env/catalog.json': es.catalogResponse,
  'env/me.json': es.meEnvResponse,
  'env/me-machines.json': es.meMachinesResponse,
  'env/me-runs.json': es.meRunsResponse,
  'env/me-run.json': es.meRunResponse,
  'env/runs.json': es.envRunsResponse,
  'env/run.json': es.envRunResponse,
  'env/try.json': es.envTryResponse,
  'env/order.json': es.envOrderResponse,
};

describe('hub contract fixtures', () => {
  it('KPI fixtures carry all twelve, once each', () => {
    for (const f of ['analytics/kpis-admin.json', 'analytics/me-kpis-member.json']) {
      const kpis = (fixtures[`./fixtures/${f}`] as A.KpisResponse).kpis.map((k) => k.kpi);
      expect(new Set(kpis).size, f).toBe(12);
    }
  });
  it('the effective limit names its binding rule first, and the rule exists', () => {
    const me = fixtures['./fixtures/limits/vera.json'] as L.MeLimitsResponse;
    const ids = new Set(me.rules.map((r) => r.id));
    for (const e of me.effective) {
      expect(e.ruleIds[0]).toBe(e.bindingRuleId);
      expect(e.ruleIds.every((id) => ids.has(id))).toBe(true);
    }
  });
  it('every fixture has a schema', () => {
    const files = Object.keys(fixtures).map((p) => p.replace('./fixtures/', ''));
    expect(files.filter((f) => !(f in schemaOf))).toEqual([]);
  });
  for (const [file, schema] of Object.entries(schemaOf)) {
    it(file, () => {
      const data = fixtures[`./fixtures/${file}`];
      expect(data, `${file} is missing`).toBeDefined();
      expect(schema.check(data, file)).toEqual([]);
    });
  }
});
