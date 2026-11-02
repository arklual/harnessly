// Машины on `GET …/machines` and the actions of /api/machines/{id} (docs/hub/analytics.md, «Machines»). The hub holds no
// load or hardware for a machine and no cloud provider, so those stay empty; a server's resize has nothing to go to.
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
  OrgMachine,
  PersonRef,
} from '../machines';
import { http, orgPath } from './http';
import { capsOf } from './shell';

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

export const orgMachineOf = (m: HubMachine, view: OrgView): OrgMachine => {
  const tenant = view.tenants.find((t) => t.id === m.tenantId);
  const team = view.teams.find((t) => t.id === m.teamId);
  const state = stateOf(m);
  const tenantCaps = capsOf(view.me, { kind: 'tenant', id: m.tenantId });
  const teamCaps = m.teamId ? capsOf(view.me, { kind: 'team', id: m.teamId }) : undefined;
  const canAdmin = tenantCaps?.manage ?? false;
  return {
    id: m.id,
    name: m.name,
    ownerName: null,
    purpose: null,
    kind: 'shared',
    tenantId: m.tenantId,
    tenantSlug: tenant?.slug ?? '',
    tenantName: tenant?.name ?? '',
    teamId: m.teamId,
    teamName: team?.name ?? null,
    state,
    since: state === 'online' ? null : (m.disabledAt ?? m.lastActiveAt ?? m.lastSeenAt),
    disabledBy: null,
    disabledAt: m.disabledAt,
    busy: null,
    chats: m.status === 'online' ? m.chatsRunning : null,
    cpu: null,
    ram: null,
    disk: null,
    hw: { cores: 0, ramGb: 0, diskGb: 0, note: m.hostname },
    cloud: false,
    runnerVersion: m.agentVersion,
    runnerOutdated: m.problems.includes('agent_old'),
    weekCostUsd: null,
    monthPriceRub: null,
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

const notYet = () => Promise.reject(new OrgApiError('not_ready', 'the hub has no cloud provider to ask'));

export const liveMachines = (ctx: () => MachinesCtx): MachinesApi => {
  const list = async (): Promise<HubMachine[]> => {
    const c = ctx();
    const r = await http.get<MachinesResponse>(orgPath(c.org, '/machines'), {
      scope: `${c.scope.kind}:${c.scope.id}`,
    });
    return r.machines;
  };

  const detail = async (id: string): Promise<MachineDetail> => {
    const c = ctx();
    const found = (await list()).find((m) => m.id === id);
    if (!found) throw new OrgApiError('not_found', 'machine not in this scope');
    const members = await http
      .get<{ members: Member[] }>(orgPath(c.org, `/scopes/tenant/${found.tenantId}/members`))
      .then((r) => r.members)
      .catch((e: unknown) => {
        // A lead may read only their team; the people to pick from are then the team's.
        if (e instanceof OrgApiError && e.code === 'forbidden' && found.teamId) {
          return http
            .get<{ members: Member[] }>(orgPath(c.org, `/scopes/team/${found.teamId}/members`))
            .then((r) => r.members);
        }
        throw e;
      });
    const candidates: PersonRef[] = members.map((m) => ({ id: m.userId, name: m.name || m.email }));
    const team = c.view.teams.find((t) => t.id === found.teamId);
    const machine = orgMachineOf(found, c.view);
    return {
      machine,
      series: { cpu: [], ram: [], disk: [] },
      plan: null,
      access: accessOf(found),
      candidates,
      teamLabel: machine.teamName,
      audience: { team: team ? team.memberCount : null, tenant: members.length },
    };
  };

  const post = async (id: string, action: string, body: unknown = {}) => {
    await http.post(`/api/machines/${id}/${action}`, body);
    return detail(id);
  };

  return {
    machinesList: async () => buildMachinesList(await list(), ctx()),
    machineDetail: detail,
    resizeMachine: notYet,
    restartMachine: (id, what, when) => {
      const body: RestartRequest = { what: what === 'machine' ? 'vm' : 'runner', when };
      return post(id, 'restart', body);
    },
    // The hub takes a restart and has no way yet to call it back.
    cancelRestart: notYet,
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
