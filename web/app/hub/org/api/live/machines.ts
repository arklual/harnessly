// Машины on `GET …/machines` and the actions of /api/machines/{id} (docs/hub/analytics.md, «Machines»). The load, the
// hardware, the rented server, the week's spend and the operations in flight come with the machine when the hub has
// them (machine resources, HRN-633); what it does not send stays null and the screen shows «—».
import type { HubMachine, MachinesResponse, RestartRequest } from '../../../../hub-api/analytics';
import type { Member, OrgView, Scope as ApiScope } from '../../../../hub-api/orgs';
import { OrgApiError } from '../errors';
import type {
  MachineAccess,
  MachineDetail,
  MachineGroup,
  MachinesApi,
  MachinesList,
  MachinesSummary,
  MovePlan,
  OrgMachine,
  PersonRef,
  ResourcePlan,
  ResourceQuote,
} from '../machines';
import { contractGet, http, orgPath } from './http';
import { capsOf } from './shell';
import {
  busyOf,
  gbOf,
  hourly,
  monthRub,
  rentOf,
  type WireExtras,
  type WireMoney,
  type WirePoint,
} from './machines-wire';

export interface MachinesCtx {
  org: string;
  view: OrgView;
  scope: ApiScope;
}

const stateOf = (m: HubMachine): OrgMachine['state'] => {
  if (m.disabledAt) return 'disabled';
  if (m.status !== 'online') return 'offline';
  return m.problems.includes('idle_week') ? 'idle' : 'online';
};

export const orgMachineOf = (raw: HubMachine, view: OrgView): OrgMachine => {
  const m = raw;
  const x = raw as unknown as WireExtras;
  const rent = rentOf(x.cloud);
  const hw = x.hardware ?? null;
  const owner = m.access.length === 1 && m.access[0]?.kind === 'user' ? m.access[0] : null;
  const tenant = view.tenants.find((t) => t.id === m.tenantId);
  const team = view.teams.find((t) => t.id === m.teamId);
  const state = stateOf(m);
  const tenantCaps = capsOf(view.me, { kind: 'tenant', id: m.tenantId });
  const teamCaps = m.teamId ? capsOf(view.me, { kind: 'team', id: m.teamId }) : undefined;
  const canAdmin = tenantCaps?.manage ?? false;
  return {
    id: m.id,
    name: m.name,
    ownerName: owner?.label ?? null,
    purpose: null,
    kind: owner ? 'personal' : 'shared',
    tenantId: m.tenantId,
    tenantSlug: tenant?.slug ?? '',
    tenantName: tenant?.name ?? '',
    teamId: m.teamId,
    teamName: team?.name ?? null,
    state,
    since: state === 'online' ? null : (m.disabledAt ?? m.lastActiveAt ?? m.lastSeenAt),
    disabledBy: null,
    disabledAt: m.disabledAt,
    busy: busyOf(x.activeOps),
    chats: m.status === 'online' ? m.chatsRunning : null,
    cpu: state === 'offline' ? null : (x.load?.cpuPct ?? null),
    ram: state === 'offline' ? null : (x.load?.ramPct ?? null),
    disk: state === 'offline' ? null : (x.load?.diskPct ?? null),
    hw: {
      cores: hw?.cores || rent?.cpu || 0,
      ramGb: gbOf(hw?.ramBytes) || rent?.ramGb || 0,
      diskGb: gbOf(hw?.diskBytes) || rent?.diskGb || 0,
      note: hw?.laptop ? (hw.model ?? '') : '',
    },
    cloud: rent !== null,
    runnerVersion: m.agentVersion,
    runnerOutdated: m.problems.includes('agent_old'),
    weekCostUsd: x.week?.spend?.usd ?? null,
    monthPriceRub: monthRub(rent?.price ?? null),
    mine: owner !== null && owner.id === view.me.userId,
    weekChats: x.week?.chats ?? null,
    os: hw?.os || null,
    rent: rent ? { provider: rent.provider, plan: rent.plan, price: rent.price } : null,
    managed: x.managed ?? null,
    opensRuntime: m.opensRuntime ?? true,
    canManage: canAdmin || (teamCaps?.manage ?? false),
    canAdmin,
    needs: canAdmin ? null : 'tenant_admin',
  };
};

/** A list of the organization goes by tenant, of a tenant by team. */
const groupsOf = (machines: OrgMachine[], ctx: MachinesCtx): MachineGroup[] => {
  const byTenant = ctx.scope.kind === 'org';
  const keys = [...new Set(machines.map((m) => (byTenant ? m.tenantId : (m.teamId ?? ''))))];
  return keys.map((key) => {
    const own = machines.filter((m) => (byTenant ? m.tenantId : (m.teamId ?? '')) === key);
    const first = own[0];
    const count = (s: OrgMachine['state']) => own.filter((m) => m.state === s).length;
    return {
      key: key || 'none',
      name: byTenant ? (first?.tenantName ?? '') : (first?.teamName ?? 'Без команды'),
      sub: !byTenant && !key ? 'машины тенанта без команды' : '',
      tenantSlug: byTenant ? (first?.tenantSlug ?? null) : null,
      total: own.length,
      offline: count('offline'),
      idle: count('idle'),
      online: count('online'),
      weekCostUsd: null,
      machines: own,
    };
  });
};

export const buildMachinesList = (raw: HubMachine[], ctx: MachinesCtx): MachinesList => {
  const machines = raw.map((m) => orgMachineOf(m, ctx.view));
  const count = (s: OrgMachine['state']) => machines.filter((m) => m.state === s).length;
  const summary: MachinesSummary = {
    total: machines.length,
    online: count('online'),
    idleWeek: count('idle'),
    offline: count('offline'),
    chatsNow: machines.reduce((n, m) => n + (m.chats ?? 0), 0),
    chatsToday: null,
    cpuMean: null,
    hottest: null,
    diskHot: null,
    outdatedRunners: machines.filter((m) => m.runnerOutdated).length,
    tenantsOffline: new Set(machines.filter((m) => m.state === 'offline').map((m) => m.tenantId)).size,
    onlineSeries: [],
    chatsSeries: [],
    loadSeries: [],
  };
  return {
    summary,
    groups: groupsOf(machines, ctx),
    tenants: ctx.scope.kind === 'org' ? ctx.view.tenants.map((t) => ({ slug: t.slug, name: t.name })) : [],
    mlCoreUrl: null,
  };
};

/** Empty access in the contract is the whole tenant. */
export const accessOf = (m: HubMachine): MachineAccess => {
  const team = m.access.find((a) => a.kind === 'team');
  if (m.access.length === 0) return { mode: 'tenant', people: [] };
  if (team && m.access.length === 1) return { mode: 'team', people: [] };
  return {
    mode: 'pick',
    people: m.access.filter((a) => a.kind === 'user').map((a) => ({ id: a.id, name: a.label })),
  };
};

export const accessBody = (
  m: HubMachine,
  access: { mode: MachineAccess['mode']; personIds: string[] },
): { kind: 'user' | 'team'; id: string }[] => {
  if (access.mode === 'tenant') return [];
  if (access.mode === 'pick') return access.personIds.map((id) => ({ kind: 'user' as const, id }));
  if (access.mode === 'team' && m.teamId) return [{ kind: 'team', id: m.teamId }];
  throw new OrgApiError('invalid_request', 'Сначала передайте машину команде.');
};

interface WirePlan {
  id: string;
  name: string;
  cpu: number;
  ramGB: number;
  diskGB: number;
}

interface WireResources {
  managed: boolean;
  provider: { name: string; supported: boolean };
  providerError?: string | null;
  plans: WirePlan[];
  consoleUrl?: string | null;
}

const money = (m: WireMoney | null | undefined) => (m ? { amount: m.amount, currency: m.currency } : null);

type WireAccess = { label: string };
type WireMovePlan = Omit<MovePlan, 'teamCleared' | 'teamAssigned' | 'accessRevoked' | 'limits' | 'kept'> & {
  teamCleared: WireAccess | null;
  teamAssigned: WireAccess | null;
  accessRevoked: WireAccess[] | null;
  limits: MovePlan['limits'] | null;
  kept: string[] | null;
};

const movePlanOf = ({ plan: p }: { plan: WireMovePlan }): MovePlan => ({
  from: p.from,
  to: p.to,
  noop: p.noop,
  teamCleared: p.teamCleared?.label ?? null,
  teamAssigned: p.teamAssigned?.label ?? null,
  accessRevoked: (p.accessRevoked ?? []).map((a) => a.label),
  limits: p.limits ?? { fromRules: 0, toRules: 0 },
  kept: p.kept ?? [],
});

const moveBody = (tenantId: string, teamId: string | undefined, dryRun: boolean) => ({
  tenantId,
  ...(teamId ? { teamId } : {}),
  ...(dryRun ? { dry_run: true } : {}),
});

export const liveMachines = (ctx: () => MachinesCtx): MachinesApi => {
  const list = async (): Promise<HubMachine[]> => {
    const c = ctx();
    const r = await http.get<MachinesResponse>(orgPath(c.org, '/machines'), {
      scope: `${c.scope.kind}:${c.scope.id}`,
    });
    return r.machines;
  };

  /** The last day of load; a hub without metrics leaves the three charts empty. */
  const seriesOf = async (id: string): Promise<MachineDetail['series']> => {
    try {
      const r = await contractGet<{ points: WirePoint[] }>(`/api/machines/${id}/metrics`, { range: '24h' });
      return {
        cpu: hourly(r.points, 'cpuAvg'),
        ram: hourly(r.points, 'ramAvg'),
        disk: hourly(r.points, 'diskAvg'),
      };
    } catch {
      return { cpu: [], ram: [], disk: [] };
    }
  };

  /** The sizes the provider offers (the stops of the resize sliders); none while the feature is off or the machine is not a server. */
  const resourcesOf = async (
    id: string,
    machine: OrgMachine,
  ): Promise<{ plan: ResourcePlan | null; consoleUrl: string | null; providerError: string | null }> => {
    const none = { plan: null, consoleUrl: null, providerError: null };
    if (!machine.cloud) return none;
    try {
      const r = await contractGet<WireResources>(`/api/machines/${id}/resources`);
      const sorted = (f: (p: WirePlan) => number) =>
        [...new Set(r.plans.map(f).filter((n) => n > 0))].sort((a, b) => a - b);
      const cores = sorted((p) => p.cpu);
      const ramGb = sorted((p) => p.ramGB);
      const diskGb = sorted((p) => p.diskGB);
      const ok =
        r.managed && r.provider.supported && cores.length > 0 && ramGb.length > 0 && diskGb.length > 0;
      const used = (pct: number | null, total: number) =>
        pct === null ? 0 : Math.round((pct / 100) * total);
      return {
        plan: ok
          ? {
              cores,
              ramGb,
              diskGb,
              price: { core: 0, ramGb: 0, diskGb: 0 },
              usedRamGb: used(machine.ram, machine.hw.ramGb),
              usedDiskGb: used(machine.disk, machine.hw.diskGb),
            }
          : null,
        consoleUrl: r.consoleUrl ?? null,
        providerError: r.providerError ?? null,
      };
    } catch (e) {
      if (e instanceof OrgApiError && e.code === 'not_ready') return none;
      return { ...none, providerError: e instanceof Error ? e.message : null };
    }
  };

  const detail = async (id: string): Promise<MachineDetail> => {
    const c = ctx();
    const found = (await list()).find((m) => m.id === id);
    if (!found) throw new OrgApiError('not_found', 'machine not in this scope');
    // Ask only what the person may read, as the hub decides it (viewMachine): the tenant's people for its
    // admin, the team's for its lead. A tenant machine outside a lead's team is one they only use: no people
    // to pick, no load charts, no sizes.
    const can = (key: string) => c.view.me.scopes[key as keyof typeof c.view.me.scopes]?.view === true;
    const tenantView = can(`tenant:${found.tenantId}`);
    const teamView = !!found.teamId && can(`team:${found.teamId}`);
    const peopleOf = (path: string) =>
      http
        .get<{ members: Member[] }>(orgPath(c.org, path))
        .then((r) => r.members)
        .catch((e: unknown) => {
          if (e instanceof OrgApiError && e.code === 'forbidden') return [];
          throw e;
        });
    const members: Member[] = tenantView
      ? await peopleOf(`/scopes/tenant/${found.tenantId}/members`)
      : teamView
        ? await peopleOf(`/scopes/team/${found.teamId}/members`)
        : [];
    const candidates: PersonRef[] = members.map((m) => ({ id: m.userId, name: m.name || m.email }));
    const team = c.view.teams.find((t) => t.id === found.teamId);
    const machine = orgMachineOf(found, c.view);
    const [series, res] =
      tenantView || teamView
        ? await Promise.all([seriesOf(id), resourcesOf(id, machine)])
        : [
            { cpu: [], ram: [], disk: [] },
            { plan: null, consoleUrl: null, providerError: null },
          ];
    return {
      machine,
      series,
      plan: res.plan,
      access: accessOf(found),
      candidates,
      teamLabel: machine.teamName,
      audience: { team: team ? team.memberCount : null, tenant: members.length },
      consoleUrl: res.consoleUrl,
      providerError: res.providerError,
    };
  };

  const post = async (id: string, action: string, body: unknown = {}) => {
    await http.post(`/api/machines/${id}/${action}`, body);
    return detail(id);
  };

  return {
    machinesList: async () => buildMachinesList(await list(), ctx()),
    machineDetail: detail,
    resizeMachine: (id, to) => post(id, 'resize', { cpu: to.cores, ramGB: to.ramGb, diskGB: to.diskGb }),
    quoteResources: async (id, to): Promise<ResourceQuote> => {
      const r = await http.post<{
        price: WireMoney | null;
        current: WireMoney | null;
        delta: WireMoney | null;
      }>(`/api/machines/${id}/resources/quote`, { cpu: to.cores, ramGB: to.ramGb, diskGB: to.diskGb });
      return { price: money(r.price), current: money(r.current), delta: money(r.delta) };
    },
    restartMachine: (id, what, when) => {
      const body: RestartRequest = { what: what === 'machine' ? 'vm' : 'runner', when };
      return post(id, 'restart', body);
    },
    // Calls back the operation that still waits (chats to finish, a restart not yet sent to the provider).
    cancelRestart: async (id) => {
      const found = (await list()).find((m) => m.id === id);
      const opId = found ? busyOf((found as unknown as WireExtras).activeOps)?.opId : undefined;
      if (!opId) throw new OrgApiError('not_found', 'nothing to cancel');
      return post(id, `ops/${opId}/cancel`);
    },
    renameMachine: async (id, name) => {
      await http.patch(`/api/machines/${id}`, { name });
      return detail(id);
    },
    planMoveMachine: async (id, tenantId, teamId) =>
      movePlanOf(
        await http.post<{ plan: WireMovePlan }>(`/api/machines/${id}/move`, moveBody(tenantId, teamId, true)),
      ),
    moveMachine: async (id, tenantId, teamId) =>
      movePlanOf(
        await http.post<{ plan: WireMovePlan }>(
          `/api/machines/${id}/move`,
          moveBody(tenantId, teamId, false),
        ),
      ),
    removeMachine: async (id) => {
      await http.del(`/api/machines/${id}`);
    },
    openMachine: (id) => contractGet<{ url: string }>(`/api/machines/${id}/open`),
    trustMachine: async (id) => {
      await http.post(`/api/machines/${id}/trust`, {});
    },
    disableMachine: (id) => post(id, 'disable'),
    enableMachine: (id) => post(id, 'enable'),
    setMachineAccess: async (id, access) => {
      const found = (await list()).find((m) => m.id === id);
      if (!found) throw new OrgApiError('not_found', 'machine not in this scope');
      await http.patch(`/api/machines/${id}`, { access: accessBody(found, access) });
      return detail(id);
    },
  };
};
